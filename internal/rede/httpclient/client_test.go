package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/rede"
)

// Estes testes só falam com um servidor httptest em loopback (nunca com a internet).

func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return newClient(options{roots: pool, allowLoopback: true, timeout: 2 * time.Second})
}

func TestGetEnviaSoOMinimo(t *testing.T) {
	var got http.Header
	var method string
	var bodyLen int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, method, bodyLen = r.Header.Clone(), r.Method, r.ContentLength
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
		_, _ = w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	for i := 0; i < 2; i++ { // sem jar: o cookie da primeira resposta não volta na segunda
		resp, err := c.Get(context.Background(), srv.URL+"/ip")
		if err != nil || resp.Status != 200 || string(resp.Body) != "203.0.113.7\n" || resp.Truncated {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
		if method != "GET" || bodyLen > 0 {
			t.Errorf("método %s corpo %d", method, bodyLen)
		}
		if got.Get("User-Agent") != "toolkit" {
			t.Errorf("User-Agent=%q", got.Get("User-Agent"))
		}
		for _, h := range []string{"Cookie", "Authorization", "Referer", "X-Forwarded-For", "Proxy-Authorization"} {
			if got.Get(h) != "" {
				t.Errorf("cabeçalho %s não pode ser enviado: %q", h, got.Get(h))
			}
		}
	}
}

func TestGetNaoSegueRedirecionamento(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("198.51.100.1"))
	}))
	defer target.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()
	resp, err := testClient(t, srv).Get(context.Background(), srv.URL+"/ip")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 302 || !strings.HasPrefix(resp.Location, target.URL) {
		t.Errorf("resp=%+v", resp)
	}
	if hits.Load() != 0 {
		t.Errorf("o redirecionamento foi seguido (%d chamadas ao destino)", hits.Load())
	}
}

func TestGetLimitaOCorpo(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("1", 1<<20)))
	}))
	defer srv.Close()
	resp, err := testClient(t, srv).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated || len(resp.Body) > rede.MaxBody+1 {
		t.Errorf("truncated=%v len=%d", resp.Truncated, len(resp.Body))
	}
	// exatamente no limite: não é truncado
	srv2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("1", rede.MaxBody)))
	}))
	defer srv2.Close()
	resp, err = testClient(t, srv2).Get(context.Background(), srv2.URL)
	if err != nil || resp.Truncated || len(resp.Body) != rede.MaxBody {
		t.Errorf("no limite: %+v %v", resp.Truncated, err)
	}
}

func TestGetPrazo(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c := newClient(options{roots: pool, allowLoopback: true, timeout: 80 * time.Millisecond})
	start := time.Now()
	if _, err := c.Get(context.Background(), srv.URL); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v em %v", err, time.Since(start))
	}
}

func TestPadraoRecusaEnderecoNaoPublico(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer srv.Close()
	_, err := New().Get(context.Background(), srv.URL) // 127.0.0.1: loopback
	if !errors.Is(err, rede.ErrNonPublicAddress) {
		t.Fatalf("err=%v, quero ErrNonPublicAddress", err)
	}
}

func TestCheckAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1:443", "10.0.0.1:443", "192.168.1.1:443", "169.254.1.1:443", "100.64.0.1:443", "[::1]:443", "[fd00::1]:443", "0.0.0.0:443", "[fe80::1%eth0]:443"} {
		if err := checkAddr(a); !errors.Is(err, rede.ErrNonPublicAddress) {
			t.Errorf("%s deveria ser recusado: %v", a, err)
		}
	}
	for _, a := range []string{"203.0.113.7:443", "[2001:db8::1]:443", "198.51.100.1:443"} {
		if err := checkAddr(a); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
}

func TestTransportDefault(t *testing.T) {
	c := New()
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transporte: %T", c.http.Transport)
	}
	if tr.Proxy != nil {
		t.Error("nenhum proxy implícito (variáveis HTTP(S)_PROXY não valem)")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("TLS 1.2+: %+v", tr.TLSClientConfig)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("o certificado SEMPRE é verificado")
	}
	if c.http.Jar != nil {
		t.Error("sem cookies")
	}
	if c.http.Timeout <= 0 || c.http.Timeout > rede.QueryTimeout() {
		t.Errorf("prazo: %v", c.http.Timeout)
	}
	if !tr.DisableCompression || !tr.DisableKeepAlives {
		t.Error("sem compressão (bomba de descompressão) e sem conexão reaproveitada")
	}
}

// O cliente real implementa rede.Fetcher (checado em tempo de compilação).
var _ rede.Fetcher = (*Client)(nil)
