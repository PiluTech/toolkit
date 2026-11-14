// Package httpclient é o cliente HTTP real das consultas de IP público e de portal cativo (rede.Fetcher). Mora num
// subpacote para que internal/rede continue SEM importar net/http. Faz UM GET: TLS 1.2+ com o certificado sempre
// verificado, sem proxy implícito, sem cookies, sem seguir redirecionamentos (devolve o 3xx e o Location), sem
// compressão e sem reaproveitar conexão, com prazo curto e limite de corpo, só o User-Agent genérico "toolkit" e, como
// proteção extra contra um nome que resolva para a rede local, recusa conectar a endereço não público.
package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/PiluTech/toolkit/internal/rede"
)

// Client implementa rede.Fetcher.
type Client struct{ http *http.Client }

type options struct {
	roots         *x509.CertPool // nil = as raízes do sistema
	allowLoopback bool           // só os testes (servidor em 127.0.0.1)
	timeout       time.Duration  // 0 = rede.QueryTimeout()
}

// New devolve o cliente real.
func New() *Client { return newClient(options{}) }

func newClient(o options) *Client {
	timeout := o.timeout
	if timeout == 0 {
		timeout = rede.QueryTimeout()
	}
	d := &net.Dialer{Timeout: timeout}
	if !o.allowLoopback {
		d.Control = func(_, address string, _ syscall.RawConn) error { return checkAddr(address) }
	}
	tr := &http.Transport{
		Proxy:                  nil, // nada de proxy vindo do ambiente
		DialContext:            d.DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.roots},
		ForceAttemptHTTP2:      true,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 8 << 10,
	}
	return &Client{http: &http.Client{
		Transport:     tr,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// checkAddr recusa o endereço (host:porta, já resolvido) que não é público.
func checkAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return rede.ErrNonPublicAddress
	}
	a, err := netip.ParseAddr(host)
	if err != nil || a.Zone() != "" || !rede.IsPublicIP(a.String()) {
		return rede.ErrNonPublicAddress
	}
	return nil
}

// Get faz o GET. A URL já foi validada por rede.NewPlan; aqui só se monta e envia.
func (c *Client) Get(ctx context.Context, rawURL string) (rede.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return rede.Response{}, fmt.Errorf("requisição inválida: %w", err)
	}
	req.Header.Set("User-Agent", rede.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return rede.Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, rede.MaxBody+1))
	if err != nil {
		return rede.Response{}, err
	}
	out := rede.Response{Status: resp.StatusCode, Location: resp.Header.Get("Location")}
	if len(body) > rede.MaxBody {
		out.Truncated, body = true, body[:rede.MaxBody]
	}
	out.Body = body
	return out, nil
}
