package cli

import "testing"

// A checagem passiva de versão NUNCA leva o token do ambiente (GITHUB_TOKEN/GH_TOKEN): 60/h sem token bastam.
func TestPassiveUpdateCheckNeverCarriesTheGitHubToken(t *testing.T) {
	deps := Deps{GitHubToken: func() string { return "ghp_segredo.do.ambiente" }}
	cfg := passiveConfig(deps, "v0.2.0")
	if cfg.Token != nil {
		t.Fatal("a checagem passiva não pode carregar o token")
	}
	if cfg.Current != "v0.2.0" {
		t.Fatalf("current: %q", cfg.Current)
	}
	// o `toolkit update` (pedido explícito) continua usando o token
	uc, err := (UpdateDeps{Exe: func() (string, error) { return "/x/toolkit", nil }}).config(deps)
	if err != nil {
		t.Fatal(err)
	}
	if uc.Token == nil || uc.Token() != "ghp_segredo.do.ambiente" {
		t.Fatal("o update explícito mantém o token")
	}
}
