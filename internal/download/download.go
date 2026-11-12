// Package download baixa arquivos e metadados por https com as regras de segurança do toolkit:
//   - toda URL passa por catalog.CheckURL (https, host público com ponto, sem userinfo, sem IP literal, porta 443)
//     antes da requisição e de novo a cada redirect (no máximo MaxRedirects saltos);
//   - o cliente padrão só conecta a endereços públicos (CheckPublicIP no Dialer.Control, sobre o IP já resolvido),
//     então um nome público que resolve para 127.0.0.1 ou 169.254.169.254 também é recusado;
//   - o SHA-256 é OBRIGATÓRIO e conferido sobre os bytes recebidos (sem descompressão automática) ANTES de o
//     arquivo existir sob o nome final: divergência apaga o .part e devolve ErrHashMismatch, sem retry;
//   - tamanho máximo (Content-Length e leitura limitada), timeout por tentativa, retry só para falha de rede,
//     429 e 5xx; o arquivo vai para um diretório temporário 0700 criado por NewTempDir, com nome próprio
//     (SafeName), nunca o que o servidor sugerir.
//
// O proxy do ambiente (HTTP_PROXY etc.) NÃO é usado pelo cliente padrão: a checagem do IP conectado não teria
// sentido com um proxy no meio. Quem precisar de proxy corporativo injeta o próprio Client.
package download

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/PiluTech/toolkit/internal/buildinfo"
	"github.com/PiluTech/toolkit/internal/catalog"
)

// Erros tipados.
var (
	// ErrNoHash: não há SHA-256 esperado. Nunca se baixa para usar sem hash.
	ErrNoHash = errors.New("sha256 obrigatório para baixar")
	// ErrBadHash: o SHA-256 esperado não tem 64 caracteres hexadecimais.
	ErrBadHash = errors.New("sha256 inválido (esperado 64 caracteres hexadecimais)")
	// ErrHashMismatch: o hash do que chegou não é o esperado. Não retenta.
	ErrHashMismatch = errors.New("o sha256 do download não confere")
	// ErrTooLarge: o conteúdo passa do tamanho máximo.
	ErrTooLarge = errors.New("download maior que o limite")
	// ErrURL: a URL (inicial ou de um redirect) viola a política de URL.
	ErrURL = errors.New("url recusada")
	// ErrStatus: resposta HTTP que não é 200.
	ErrStatus = errors.New("resposta http inesperada")
)

// StatusError é a resposta http que não é 200; errors.Is(err, ErrStatus) vale e Code traz o status.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%v: %s", ErrStatus, e.Status) }

// Is faz errors.Is(err, ErrStatus) valer para qualquer StatusError.
func (*StatusError) Is(target error) bool { return target == ErrStatus }

// Padrões.
const (
	MaxRedirects      = 5
	DefaultMaxSize    = 512 << 20 // 512 MiB
	DefaultTimeout    = 30 * time.Minute
	DefaultRetries    = 2
	maxSafeNameLength = 100
)

var hexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Downloader baixa com as regras do pacote. O zero value serve: usa NewClient e catalog.CheckURL.
type Downloader struct {
	// Client é o cliente HTTP (nil = NewClient()). Uma cópia dele ganha o CheckRedirect do Downloader.
	Client *http.Client
	// CheckURL valida cada URL (nil = catalog.CheckURL(u, false)). Só testes trocam: o servidor de teste
	// local jamais passaria na política real.
	CheckURL func(string) error
	// Timeout vale por tentativa (0 = DefaultTimeout).
	Timeout time.Duration
	// Retries é o número de novas tentativas para falha de rede, 429 e 5xx (0 = nenhuma). Hash divergente,
	// tamanho excedido e 4xx nunca são retentados.
	Retries int
	// Backoff devolve a espera antes da tentativa n (1 = primeira nova). Nil = 2s*n.
	Backoff func(attempt int) time.Duration
}

// Request descreve um download verificado.
type Request struct {
	URL     string
	SHA256  string      // obrigatório, 64 hex
	MaxSize int64       // 0 = DefaultMaxSize
	Dir     string      // File: diretório (de NewTempDir) onde o arquivo é gravado
	Name    string      // File: nome do arquivo (passa por SafeName)
	Header  http.Header // cabeçalhos extras (ex.: Accept); nunca credenciais para hosts de terceiros
}

// Result descreve um File concluído.
type Result struct {
	Path   string // arquivo final, já com o hash conferido
	SHA256 string // hash em minúsculas
	Size   int64
}

// NewTempDir cria um diretório temporário com modo 0700 (explícito: não depende do umask) e devolve a função
// que o remove. No Windows o modo não se aplica: a pasta herda a ACL do diretório temporário do usuário.
func NewTempDir() (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "toolkit-dl-")
	if err != nil {
		return "", nil, fmt.Errorf("diretório temporário: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("diretório temporário: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SafeName devolve um nome de arquivo seguro a partir do caminho de uma URL: só o último segmento, só
// [A-Za-z0-9._-], sem "." nem "-" no início, até 100 caracteres; vazio vira "download".
func SafeName(urlPath string) string {
	name := path.Base(path.Clean("/" + urlPath))
	name = unsafeNameRe.ReplaceAllString(name, "_")
	name = strings.TrimLeft(name, ".-_")
	if len(name) > maxSafeNameLength {
		name = name[:maxSafeNameLength]
	}
	if name == "" || name == "/" {
		return "download"
	}
	return name
}

// CheckPublicIP recusa endereços que não são destino legítimo de um download: loopback, privados (RFC 1918 e
// ULA), CGNAT (100.64/10), link-local (inclusive 169.254.169.254 dos metadados de nuvem), multicast e não
// especificado. IPv6 mapeado em IPv4 é desmapeado antes.
func CheckPublicIP(a netip.Addr) error {
	a = a.Unmap()
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || cgnat.Contains(a) {
		return fmt.Errorf("endereço %s não público", a)
	}
	return nil
}

// NewClient devolve o cliente padrão: sem proxy, sem descompressão automática, cada conexão confere o IP.
func NewClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("endereço %q: %w", address, err)
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("endereço %q: %w", address, err)
			}
			return CheckPublicIP(ip)
		},
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}}
}

func (d *Downloader) check(u string) error {
	var err error
	if d.CheckURL != nil {
		err = d.CheckURL(u)
	} else {
		err = catalog.CheckURL(u, false)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrURL, err)
	}
	return nil
}

func (d *Downloader) client() *http.Client {
	base := d.Client
	if base == nil {
		base = NewClient()
	}
	c := *base
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects {
			return fmt.Errorf("%w: mais de %d redirecionamentos", ErrURL, MaxRedirects)
		}
		return d.check(req.URL.String())
	}
	return &c
}

func (d *Downloader) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

func (d *Downloader) backoff(n int) time.Duration {
	if d.Backoff != nil {
		return d.Backoff(n)
	}
	return time.Duration(n) * 2 * time.Second
}

// transient marca um erro que vale uma nova tentativa.
type transient struct{ err error }

func (t transient) Error() string { return t.err.Error() }
func (t transient) Unwrap() error { return t.err }

// get faz UMA requisição e despeja o corpo (limitado a max) em dst.
func (d *Downloader) get(ctx context.Context, rawURL string, header http.Header, limit int64, dst io.Writer) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrURL, err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// "identity": o servidor não comprime e o Go não descomprime; o hash vale para os bytes servidos.
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", buildinfo.Name+"/"+buildinfo.Version)
	resp, err := d.client().Do(req)
	if err != nil {
		if errors.Is(err, ErrURL) {
			return 0, err
		}
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			return 0, ctx.Err()
		}
		return 0, transient{fmt.Errorf("baixando: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var e error = &StatusError{Code: resp.StatusCode, Status: resp.Status}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return 0, transient{e}
		}
		return 0, e
	}
	if resp.ContentLength > limit {
		return 0, fmt.Errorf("%w: %d bytes, limite %d", ErrTooLarge, resp.ContentLength, limit)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return n, transient{fmt.Errorf("lendo o corpo: %w", err)}
	}
	if n > limit {
		return n, fmt.Errorf("%w: passou de %d bytes", ErrTooLarge, limit)
	}
	return n, nil
}

// retry roda op até 1+Retries vezes enquanto o erro for transitório. reset limpa o destino antes de repetir.
func (d *Downloader) retry(ctx context.Context, op func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d.backoff(attempt)):
			}
		}
		err = op()
		var tr transient
		if err == nil || !errors.As(err, &tr) || attempt >= d.Retries {
			break
		}
	}
	var tr transient
	if errors.As(err, &tr) {
		return tr.err
	}
	return err
}

func expectedHash(s string) (string, error) {
	if s == "" {
		return "", ErrNoHash
	}
	if !hexRe.MatchString(s) {
		return "", ErrBadHash
	}
	return strings.ToLower(s), nil
}

func (r Request) max() int64 {
	if r.MaxSize > 0 {
		return r.MaxSize
	}
	return DefaultMaxSize
}

func sameHash(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// File baixa para Dir/SafeName(Name), confere o SHA-256 e só então move o arquivo para o nome final (modo 0600).
// Em qualquer falha nada fica no diretório.
func (d *Downloader) File(ctx context.Context, r Request) (Result, error) {
	want, err := expectedHash(r.SHA256)
	if err != nil {
		return Result{}, err
	}
	if err := d.check(r.URL); err != nil {
		return Result{}, err
	}
	if r.Dir == "" {
		return Result{}, errors.New("download: diretório de destino vazio")
	}
	final := filepath.Join(r.Dir, SafeName(r.Name))
	part := final + ".part"
	var size int64
	var got string
	err = d.retry(ctx, func() error {
		f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("gravando %s: %w", part, err)
		}
		h := sha256.New()
		size, err = d.get(ctx, r.URL, r.Header, r.max(), io.MultiWriter(f, h))
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("gravando %s: %w", part, cerr)
		}
		got = hex.EncodeToString(h.Sum(nil))
		return err
	})
	if err == nil && !sameHash(got, want) {
		err = fmt.Errorf("%w: esperado %s, recebido %s", ErrHashMismatch, want, got)
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		_ = os.Remove(part)
		_ = os.Remove(final)
		return Result{}, err
	}
	return Result{Path: final, SHA256: got, Size: size}, nil
}

// Bytes baixa para a memória (limite MaxSize), confere o SHA-256 e só então devolve os bytes.
func (d *Downloader) Bytes(ctx context.Context, r Request) ([]byte, error) {
	want, err := expectedHash(r.SHA256)
	if err != nil {
		return nil, err
	}
	if err := d.check(r.URL); err != nil {
		return nil, err
	}
	var buf limitedBuffer
	err = d.retry(ctx, func() error {
		buf.reset()
		_, err := d.get(ctx, r.URL, r.Header, r.max(), &buf)
		return err
	})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(buf.b)
	if got := hex.EncodeToString(sum[:]); !sameHash(got, want) {
		return nil, fmt.Errorf("%w: esperado %s, recebido %s", ErrHashMismatch, want, got)
	}
	return buf.b, nil
}

// Metadata baixa SEM hash esperado e só serve para metadados que nunca são executados nem instalados (a
// resposta da API de releases). Mesmas regras de URL, tamanho e timeout. Quem usar o conteúdo para decidir
// algo de segurança tem de ancorar a decisão em outro lugar (um sha256 fixado no catálogo).
func (d *Downloader) Metadata(ctx context.Context, rawURL string, maxSize int64, header http.Header) ([]byte, error) {
	if err := d.check(rawURL); err != nil {
		return nil, err
	}
	var buf limitedBuffer
	err := d.retry(ctx, func() error {
		buf.reset()
		_, err := d.get(ctx, rawURL, header, maxSize, &buf)
		return err
	})
	if err != nil {
		return nil, err
	}
	return buf.b, nil
}

type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) { l.b = append(l.b, p...); return len(p), nil }
func (l *limitedBuffer) reset()                      { l.b = l.b[:0] }
