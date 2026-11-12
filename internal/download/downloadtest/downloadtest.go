// Package downloadtest monta um Downloader ligado a um servidor TLS local (httptest), só para testes: as URLs
// continuam realistas (https://example.com/...) e passam inteiras pela catalog.CheckURL de produção; o dial é
// que vai sempre para o listener local, e o certificado do httptest cobre example.com. Nenhum teste toca a rede.
package downloadtest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PiluTech/toolkit/internal/download"
)

// New devolve o Downloader e o contador de requisições recebidas pelo servidor.
func New(t testing.TB, h http.Handler) (*download.Downloader, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	tr.TLSClientConfig.ServerName = "example.com"
	return &download.Downloader{
		Client:  &http.Client{Transport: tr},
		Backoff: func(int) time.Duration { return 0 },
	}, hits
}

// Forbidden devolve um Downloader cujo transporte falha o teste se qualquer requisição sair.
func Forbidden(t testing.TB) *download.Downloader {
	t.Helper()
	return &download.Downloader{Client: &http.Client{Transport: forbidden{t}}}
}

type forbidden struct{ t testing.TB }

func (f forbidden) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("requisição de rede não esperada: %s", r.URL)
	return nil, context.Canceled
}
