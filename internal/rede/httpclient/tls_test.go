package httpclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/rede"
)

// Verificação REAL de certificado: o cliente é o de produção (newClient, o mesmo que New() usa); o teste só liga
// allowLoopback (para alcançar 127.0.0.1) e escolhe as raízes. Nada aqui fala com a internet, só com httptest em loopback.

const segredo = "CORPO-SECRETO"

// pki gera uma CA de teste e emite certificados folha com os parâmetros pedidos.
type pki struct {
	ca    *x509.Certificate
	caKey *ecdsa.PrivateKey
	pool  *x509.CertPool
}

func novaPKI(t *testing.T) *pki {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CA de teste"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &pki{ca: ca, caKey: key, pool: pool}
}

// folha emite um certificado de servidor assinado pela CA, com os SANs e a validade dados.
func (p *pki) folha(t *testing.T, ips []net.IP, dns []string, notBefore, notAfter time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "servidor de teste"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func (p *pki) valida(t *testing.T) tls.Certificate {
	return p.folha(t, []net.IP{net.ParseIP("127.0.0.1")}, nil, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
}

// servidor sobe um servidor TLS em loopback com a configuração dada e conta quantos pedidos chegaram ao handler.
func servidor(t *testing.T, cfg *tls.Config) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(segredo))
	}))
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &hits
}

func clienteReal(roots *x509.CertPool) *Client {
	return newClient(options{roots: roots, allowLoopback: true, timeout: 2 * time.Second})
}

// recusou confere o contrato comum das recusas: erro, resposta vazia, handler nunca chamado e nada do corpo no erro.
func recusou(t *testing.T, c *Client, srv *httptest.Server, hits *atomic.Int32) error {
	t.Helper()
	resp, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("era para recusar, veio %+v", resp)
	}
	if resp.Status != 0 || len(resp.Body) != 0 || resp.Location != "" {
		t.Errorf("nenhum dado da resposta pode sair: %+v", resp)
	}
	if hits.Load() != 0 {
		t.Errorf("o handler recebeu %d pedido(s): a requisição HTTP nem deveria ter sido enviada", hits.Load())
	}
	if strings.Contains(err.Error(), segredo) {
		t.Errorf("o erro vaza o corpo: %v", err)
	}
	return err
}

func TestTLSCertificadoNaoConfiavelRecusado(t *testing.T) {
	// Certificado autoassinado do httptest, SEM ser adicionado às raízes (nil = raízes do sistema).
	srv, hits := servidor(t, nil)
	err := recusou(t, clienteReal(nil), srv, hits)
	var ua x509.UnknownAuthorityError
	if !errors.As(err, &ua) {
		t.Fatalf("quero erro de autoridade desconhecida, veio %T: %v", err, err)
	}
	var cv *tls.CertificateVerificationError
	if !errors.As(err, &cv) {
		t.Errorf("quero tls.CertificateVerificationError, veio %T", err)
	}
}

func TestTLSSemFallbackParaHTTP(t *testing.T) {
	// Um https:// recusado nunca vira http:// : um servidor HTTP puro no mesmo endereço não pode ser alcançado.
	var plain atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plain.Add(1)
		_, _ = w.Write([]byte(segredo))
	}))
	defer srv.Close()
	resp, err := clienteReal(nil).Get(context.Background(), strings.Replace(srv.URL, "http://", "https://", 1))
	if err == nil || plain.Load() != 0 || len(resp.Body) != 0 {
		t.Fatalf("servidor sem TLS não pode ser aceito: resp=%+v err=%v hits=%d", resp, err, plain.Load())
	}
	// E o caminho inverso: a URL http:// pura nunca passa pelo TLS (o cliente só faz o que a URL manda; rede.NewPlan exige https).
}

func TestTLSCertificadoConfiavelAceito(t *testing.T) {
	// Controle positivo: o MESMO servidor com certificado emitido por uma CA que o teste põe nas raízes é aceito,
	// provando que a verificação está ligada e funcionando (e não desligada).
	p := novaPKI(t)
	srv, hits := servidor(t, &tls.Config{Certificates: []tls.Certificate{p.valida(t)}})
	resp, err := clienteReal(p.pool).Get(context.Background(), srv.URL)
	if err != nil || resp.Status != 200 || string(resp.Body) != segredo || hits.Load() != 1 {
		t.Fatalf("resp=%+v err=%v hits=%d", resp, err, hits.Load())
	}
	// A mesma CA fora das raízes: recusado (a confiança vem só das raízes).
	srv2, hits2 := servidor(t, &tls.Config{Certificates: []tls.Certificate{p.valida(t)}})
	_ = recusou(t, clienteReal(nil), srv2, hits2)
}

func TestTLSNomeErradoRecusado(t *testing.T) {
	p := novaPKI(t)
	// Certificado confiável, mas só para outro nome (sem o IP 127.0.0.1 nos SANs).
	cert := p.folha(t, nil, []string{"outro.example"}, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	srv, hits := servidor(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	err := recusou(t, clienteReal(p.pool), srv, hits)
	var he x509.HostnameError
	if !errors.As(err, &he) {
		t.Fatalf("quero x509.HostnameError, veio %T: %v", err, err)
	}
}

func TestTLSCertificadoExpiradoRecusado(t *testing.T) {
	p := novaPKI(t)
	cert := p.folha(t, []net.IP{net.ParseIP("127.0.0.1")}, nil, time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	srv, hits := servidor(t, &tls.Config{Certificates: []tls.Certificate{cert}})
	err := recusou(t, clienteReal(p.pool), srv, hits)
	var ci x509.CertificateInvalidError
	if !errors.As(err, &ci) || ci.Reason != x509.Expired {
		t.Fatalf("quero CertificateInvalidError/Expired, veio %T: %v", err, err)
	}
}

func TestTLSVersaoMinima(t *testing.T) {
	p := novaPKI(t)
	cert := p.valida(t)
	// Controle: servidor só TLS 1.2 (o mínimo) é aceito, então a recusa abaixo é pela versão e não por outro motivo.
	ok, _ := servidor(t, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
	if _, err := clienteReal(p.pool).Get(context.Background(), ok.URL); err != nil {
		t.Fatalf("TLS 1.2 deveria ser aceito: %v", err)
	}
	// Servidor que só fala TLS 1.1: recusado, handler nunca chamado.
	velho, hits := servidor(t, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	err := recusou(t, clienteReal(p.pool), velho, hits)
	if !strings.Contains(strings.ToLower(err.Error()), "protocol version") && !strings.Contains(strings.ToLower(err.Error()), "handshake") {
		t.Errorf("quero falha de versão/handshake, veio: %v", err)
	}
}

// A exceção de loopback e as raízes são parâmetros de newClient SÓ para teste: New() (produção) não as usa.
func TestNewProducaoSemExcecoes(t *testing.T) {
	c := New()
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transporte: %T", c.http.Transport)
	}
	cfg := tr.TLSClientConfig
	if cfg == nil {
		t.Fatal("TLSClientConfig ausente")
	}
	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify tem de ser false")
	}
	if cfg.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion=%#x, quero >= TLS 1.2", cfg.MinVersion)
	}
	if cfg.RootCAs != nil {
		t.Error("produção usa as raízes do sistema (RootCAs nil)")
	}
	if cfg.VerifyConnection != nil || cfg.VerifyPeerCertificate != nil || cfg.GetCertificate != nil {
		t.Error("nenhum gancho de verificação customizado em produção")
	}
	if tr.Proxy != nil {
		t.Error("Proxy tem de ser nil")
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", http.NoBody)
	if err := c.http.CheckRedirect(req, []*http.Request{req}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("CheckRedirect tem de recusar seguir: %v", err)
	}
	// O dialer de produção recusa loopback/privados: TestPadraoRecusaEnderecoNaoPublico (conexão real a 127.0.0.1) e
	// TestCheckAddr (a lista). Aqui, o mesmo guarda continua ativo mesmo com raízes de teste e sem allowLoopback.
	p := novaPKI(t)
	srv, hits := servidor(t, &tls.Config{Certificates: []tls.Certificate{p.valida(t)}})
	_, err := newClient(options{roots: p.pool, timeout: 2 * time.Second}).Get(context.Background(), srv.URL)
	if !errors.Is(err, rede.ErrNonPublicAddress) || hits.Load() != 0 {
		t.Fatalf("err=%v hits=%d", err, hits.Load())
	}
}
