package update

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/PiluTech/toolkit/internal/download/downloadtest"
)

// O token só vai ao api.github.com padrão (nunca a outro host) e só se tiver o formato esperado.
func TestAPITokenOnlyGoesToTheDefaultAPIHost(t *testing.T) {
	c := Config{Token: func() string { return "ghp_abc.DEF-123" }}
	if h := c.apiHeaders(DefaultAPIBase + "/latest"); h.Get("Authorization") != "Bearer ghp_abc.DEF-123" {
		t.Fatalf("host padrão: %v", h)
	}
	for _, other := range []string{"https://example.com/api/latest", "https://api.github.com.evil.example/x", "https://evil.example/" + DefaultAPIBase} {
		if h := c.apiHeaders(other); h.Get("Authorization") != "" {
			t.Fatalf("o token vazou para %s: %v", other, h)
		}
	}
	for _, bad := range []string{"tok en", "tok\nen", "a;b", "tok\r\nX: y"} {
		c.Token = func() string { return bad }
		if h := c.apiHeaders(DefaultAPIBase + "/latest"); h.Get("Authorization") != "" {
			t.Fatalf("token inválido %q não é enviado", bad)
		}
	}
	if h := (Config{}).apiHeaders(DefaultAPIBase + "/latest"); h.Get("Authorization") != "" {
		t.Fatal("sem token, sem cabeçalho")
	}
}

func TestRateLimitedAPIGivesAFriendlyError(t *testing.T) {
	d, _ := downloadtest.New(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	}))
	c := Config{APIBase: "https://example.com/api", Downloader: d, AllowedHosts: []string{"example.com"}}
	_, err := Latest(context.Background(), c)
	if !errors.Is(err, ErrRateLimited) || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ReleaseByTag(context.Background(), c, "v0.1.0"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("ReleaseByTag: %v", err)
	}
}
