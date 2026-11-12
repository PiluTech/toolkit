package download

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newClient devolve um cliente que fala com o servidor TLS de teste mas deixa a URL realista
// (https://example.com/...): o dial vai sempre para o listener local e o certificado do httptest cobre
// example.com. Assim a CheckURL de produção roda inteira, sem nenhum modo "permitir local".
func newClient(srv *httptest.Server) *http.Client {
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	tr.TLSClientConfig.ServerName = "example.com"
	return &http.Client{Transport: tr}
}

func newDL(srv *httptest.Server) *Downloader {
	return &Downloader{Client: newClient(srv), Backoff: func(int) time.Duration { return 0 }}
}

func TestFileBaixaEConfereHash(t *testing.T) {
	body := []byte("conteúdo do instalador")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	dir, cleanup, err := NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	res, err := newDL(srv).File(context.Background(), Request{URL: "https://example.com/app.msi", SHA256: strings.ToUpper(sum(body)), MaxSize: 1 << 20, Dir: dir, Name: "app.msi"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, body) || res.Size != int64(len(body)) || res.SHA256 != sum(body) {
		t.Fatalf("resultado %+v", res)
	}
	if filepath.Dir(res.Path) != dir {
		t.Fatalf("fora do dir: %s", res.Path)
	}
	if _, err := os.Stat(res.Path + ".part"); err == nil {
		t.Fatal(".part sobrou")
	}
}

func TestHashDivergenteNaoDeixaArquivoENaoRetenta(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("adulterado"))
	}))
	defer srv.Close()
	dir, cleanup, _ := NewTempDir()
	defer cleanup()
	d := newDL(srv)
	d.Retries = 3
	_, err := d.File(context.Background(), Request{URL: "https://example.com/a.exe", SHA256: sum([]byte("original")), MaxSize: 1 << 20, Dir: dir, Name: "a.exe"})
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hash divergente foi retentado: %d requisições", hits.Load())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("sobrou arquivo: %v", entries)
	}
}

func TestSemHashOuHashInvalidoRecusaAntesDeBaixar(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	dir, cleanup, _ := NewTempDir()
	defer cleanup()
	for sha, want := range map[string]error{"": ErrNoHash, "abc": ErrBadHash, strings.Repeat("g", 64): ErrBadHash} {
		if _, err := newDL(srv).File(context.Background(), Request{URL: "https://example.com/a", SHA256: sha, MaxSize: 10, Dir: dir, Name: "a"}); !errors.Is(err, want) {
			t.Fatalf("sha %q: %v", sha, err)
		}
		if _, err := newDL(srv).Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sha, MaxSize: 10}); !errors.Is(err, want) {
			t.Fatalf("bytes sha %q: %v", sha, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("baixou sem hash: %d", hits.Load())
	}
}

func TestURLRecusadaPelaPoliticaDoCatalogo(t *testing.T) {
	d := &Downloader{Client: &http.Client{Transport: failTransport{t}}}
	for _, u := range []string{"http://example.com/a", "https://127.0.0.1/a", "https://localhost/a", "https://user:pw@example.com/a", "https://example.com:8443/a", "https://10.0.0.1/a"} {
		_, err := d.Bytes(context.Background(), Request{URL: u, SHA256: sum(nil), MaxSize: 10})
		if !errors.Is(err, ErrURL) {
			t.Errorf("%s: %v", u, err)
		}
	}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("requisição não deveria sair: %s", r.URL)
	return nil, errors.New("proibido")
}

func TestRedirectRevalidaACadaSalto(t *testing.T) {
	for name, location := range map[string]string{
		"para IP":       "https://127.0.0.1/x",
		"para http":     "http://example.com/x",
		"para userinfo": "https://u:p@example.com/x",
	} {
		t.Run(name, func(t *testing.T) {
			var other atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/x" {
					other.Add(1)
				}
				http.Redirect(w, r, location, http.StatusFound)
			}))
			defer srv.Close()
			_, err := newDL(srv).Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sum(nil), MaxSize: 10})
			if !errors.Is(err, ErrURL) {
				t.Fatalf("err = %v", err)
			}
			if other.Load() != 0 {
				t.Fatal("seguiu o redirect proibido")
			}
		})
	}
}

func TestRedirectLegitimoSegueELimiteDeSaltos(t *testing.T) {
	body := []byte("ok")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "https://cdn.example.com/b", http.StatusFound)
		case "/b":
			_, _ = w.Write(body)
		default: // laço infinito
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer srv.Close()
	got, err := newDL(srv).Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sum(body), MaxSize: 10})
	if err != nil || string(got) != "ok" {
		t.Fatalf("%q %v", got, err)
	}
	_, err = newDL(srv).Bytes(context.Background(), Request{URL: "https://example.com/loop", SHA256: sum(body), MaxSize: 10})
	if !errors.Is(err, ErrURL) {
		t.Fatalf("laço: %v", err)
	}
}

func TestTamanhoMaximo(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 100)
	for name, h := range map[string]http.HandlerFunc{
		"content-length": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(big) },
		"chunked": func(w http.ResponseWriter, _ *http.Request) {
			for range 10 {
				_, _ = w.Write(big[:10])
				w.(http.Flusher).Flush()
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewTLSServer(h)
			defer srv.Close()
			dir, cleanup, _ := NewTempDir()
			defer cleanup()
			_, err := newDL(srv).File(context.Background(), Request{URL: "https://example.com/a", SHA256: sum(big), MaxSize: 50, Dir: dir, Name: "a"})
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v", err)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("sobrou arquivo: %v", entries)
			}
		})
	}
}

func TestRespostaComprimidaEhHasheadaCrua(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("texto"))
	_ = zw.Close()
	raw := buf.Bytes()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()
	got, err := newDL(srv).Bytes(context.Background(), Request{URL: "https://example.com/a.gz", SHA256: sum(raw), MaxSize: 1 << 10})
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("não devia descomprimir: %v", err)
	}
}

func TestStatusEErrosTransitorios(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			http.Error(w, "x", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	d := newDL(srv)
	d.Retries = 2
	got, err := d.Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sum([]byte("ok")), MaxSize: 10})
	if err != nil || string(got) != "ok" || n.Load() != 3 {
		t.Fatalf("%q %v %d", got, err, n.Load())
	}
	// 404 não retenta.
	n.Store(0)
	srv2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}))
	defer srv2.Close()
	d = newDL(srv2)
	d.Retries = 2
	_, err = d.Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sum(nil), MaxSize: 10})
	var se *StatusError
	if !errors.Is(err, ErrStatus) || !errors.As(err, &se) || se.Code != http.StatusNotFound || n.Load() != 1 {
		t.Fatalf("%v %d", err, n.Load())
	}
}

func TestMetadataSemHashEnviaCabecalhos(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Teste") != "1" {
			t.Error("cabeçalho ausente")
		}
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()
	h := http.Header{}
	h.Set("X-Teste", "1")
	got, err := newDL(srv).Metadata(context.Background(), "https://example.com/api", 1<<10, h)
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := newDL(srv).Metadata(context.Background(), "https://example.com/api", 3, h); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limite: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	d := newDL(srv)
	d.Timeout = 100 * time.Millisecond
	start := time.Now()
	if _, err := d.Bytes(context.Background(), Request{URL: "https://example.com/a", SHA256: sum(nil), MaxSize: 10}); err == nil {
		t.Fatal("esperava timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout não respeitado")
	}
}

func TestNewTempDirModo0700(t *testing.T) {
	dir, cleanup, err := NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatal("não criou")
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o700 {
		t.Fatalf("modo %v", st.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("cleanup não removeu")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"/a/b/app-1.2.msi":               "app-1.2.msi",
		"/a/../../etc/passwd":            "passwd",
		"/a/-rf":                         "rf",
		"/a/.hidden":                     "hidden",
		"/a/na me;rm.exe":                "na_me_rm.exe",
		"/":                              "download",
		"":                               "download",
		"/a/" + strings.Repeat("x", 300): strings.Repeat("x", 100),
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestCheckPublicIP(t *testing.T) {
	for ip, ok := range map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
		"fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false,
	} {
		err := CheckPublicIP(netip.MustParseAddr(ip))
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, ok=%v", ip, err, ok)
		}
	}
}

func TestClientePadraoRecusaDestinoPrivado(t *testing.T) {
	// O cliente padrão (sem injeção) barra a conexão a um IP privado mesmo que a URL tenha passado.
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer func() { _ = ln.Close() }()
	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+ln.Addr().String()+"/", nil)
	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "não público") {
		t.Fatalf("err = %v", err)
	}
}
