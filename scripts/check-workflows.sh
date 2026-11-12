#!/usr/bin/env bash
# Verifica os workflows do GitHub Actions sem rede. Roda o actionlint quando existe (como o shellcheck no gate) e
# SEMPRE aplica as regras próprias de supply chain, que valem para todo workflow:
#   - toda action ("uses:") fixada por SHA de commit de 40 hex, com a versão em comentário;
#   - nada de pull_request_target, nem curl|sh / wget|sh;
#   - todo job com timeout-minutes e persist-credentials: false em todo checkout;
# runners: só os PADRÃO (ubuntu/windows/macos, gratuitos em repo público), nunca "larger runner" nem self-hosted;
# pull_request só no ci.yml (que não usa segredo, environment nem permissão de escrita); e, para o release.yml (o único
# que vê segredo):
#   - a imagem do macOS é FIXA (macos-NN, nunca macos-latest) e todo setup-go tem "cache: false";
#   - gatilho SÓ push de tag v*.*.*: sem pull_request, workflow_dispatch, schedule nem push de branch;
#   - permissions de topo vazias ({}), "contents: write" em exatamente um job e nenhum "write-all";
#   - concurrency sem cancelar release em andamento;
#   - segredos só dentro de um job com "environment: release", nunca em "echo"/"printf" de log.
# Uso: scripts/check-workflows.sh [pasta]   (padrão: .github/workflows)
set -euo pipefail
cd "$(dirname "$0")/.."
dir="${1:-.github/workflows}"
[ -d "$dir" ] || { echo "check-workflows: pasta inexistente: $dir" >&2; exit 1; }

errs=0
bad() { echo "check-workflows: $*" >&2; errs=$((errs + 1)); }

shopt -s nullglob
files=("$dir"/*.yml "$dir"/*.yaml)
[ "${#files[@]}" -gt 0 ] || { echo "check-workflows: nenhum workflow em $dir" >&2; exit 1; }

if command -v actionlint >/dev/null 2>&1; then
  actionlint "${files[@]}" || errs=$((errs + 1))
else
  echo "actionlint não encontrado: só as regras próprias" >&2
fi

for f in "${files[@]}"; do
  n="$(basename "$f")"
  # uses: só com SHA de 40 hex e comentário de versão (ações locais "./" ficam de fora)
  while IFS= read -r line; do
    case "$line" in *"uses: ./"*) continue ;; esac
    printf '%s\n' "$line" | grep -qE 'uses: *[A-Za-z0-9._/-]+@[0-9a-f]{40} +# *v?[0-9]' || bad "$n: action sem SHA de 40 hex + versão em comentário: $line"
  done < <(grep -E '^[[:space:]]*-?[[:space:]]*uses:' "$f" || true)
  grep -qE '^[[:space:]]*(pull_request_target|workflow_run)' "$f" && bad "$n: pull_request_target/workflow_run não é permitido"
  # runners: padrão hospedado, sem larger runner/self-hosted (o custo e a superfície de ataque seriam outros)
  while IFS= read -r v; do
    v="${v#*runs-on:}"; v="${v%%#*}"; v="$(printf '%s' "$v" | tr -d ' "'"'"'')"
    printf '%s' "$v" | grep -qE '^(ubuntu-(latest|2[0-9]\.[0-9]{2})|windows-(latest|20[0-9]{2})|macos-(latest|[0-9]+)|\$\{\{matrix\.os\}\})$' || bad "$n: runs-on fora da lista de runners padrão: $v"
  done < <(grep -E '^[[:space:]]*runs-on:' "$f" || true)
  grep -qiE '(large|xlarge|xl\b|self-hosted|gpu)' <(grep -E '^[[:space:]]*(runs-on:|os:|-[[:space:]]*os:)' "$f" || true) && bad "$n: larger/self-hosted runner não é permitido"
  # pull_request só no ci.yml
  if [ "$n" != ci.yml ]; then
    awk '/^on:/ {o=1; next} o && /^[A-Za-z]/ {exit} o {print}' "$f" | grep -qE '^  pull_request:' && bad "$n: pull_request só é permitido no ci.yml"
  fi
  grep -qE '(curl|wget)[^#|]*\|[[:space:]]*(sudo )?(ba)?sh' "$f" && bad "$n: curl|sh em workflow"
  jobs="$(awk '/^jobs:/ {j=1; next} j && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ {c++} END {print c+0}' "$f")"
  [ "$(grep -cE '^    timeout-minutes: [0-9]+' "$f")" = "$jobs" ] || bad "$n: todo job precisa de timeout-minutes ($jobs jobs)"
  co="$(grep -cE 'uses: *actions/checkout@' "$f" || true)"
  [ "$(grep -cE 'persist-credentials: false' "$f")" = "$co" ] || bad "$n: todo checkout precisa de persist-credentials: false"
  grep -qE '^permissions:' "$f" || bad "$n: faltam permissions de topo"
done

# ci.yml: valida PR e push na main SEM segredo, environment nem escrita
c="$dir/ci.yml"
if [ -f "$c" ]; then
  n=ci.yml
  on="$(awk '/^on:/ {o=1; next} o && /^[A-Za-z]/ {exit} o {print}' "$c")"
  printf '%s\n' "$on" | grep -qE '^  pull_request:' || bad "$n: deve validar pull_request"
  printf '%s\n' "$on" | grep -qE '^  push:' || bad "$n: deve validar push na main"
  printf '%s\n' "$on" | grep -qE '^    branches: \[main\]' || bad "$n: push só na main"
  # a palavra "secrets" em QUALQUER forma (secrets.X, secrets['X'], secrets: inherit, toJSON(secrets)) é proibida
  grep -qE '(^|[^A-Za-z0-9_])secrets([^A-Za-z0-9_]|$)' "$c" && bad "$n: o ci.yml não pode usar segredo (PR de fork roda aqui)"
  # os VALORES de matrix.os (lista inline, include e exclude) precisam estar na lista de runners padrão
  while IFS= read -r v; do
    v="${v#*os:}"; v="${v%%#*}"
    for item in $(printf '%s' "$v" | tr '[],"'"'"'' '     '); do
      printf '%s' "$item" | grep -qE '^(ubuntu-(latest|2[0-9]\.[0-9]{2})|windows-(latest|20[0-9]{2})|macos-(latest|[0-9]+))$' || bad "$n: matrix.os com runner fora da lista padrão: $item"
    done
  done < <(grep -E '^[[:space:]]*-?[[:space:]]*os:' "$c" || true)
  grep -qE '^[[:space:]]*environment:' "$c" && bad "$n: o ci.yml não pode usar environment"
  grep -qE ':[[:space:]]*write' "$c" && bad "$n: o ci.yml não pode ter permissão de escrita"
  if ! awk '/^permissions:/ {p=1; next} p && /^[A-Za-z]/ {exit} p {print}' "$c" | grep -qE '^  contents: read[[:space:]]*$'; then
    bad "$n: permissions de topo deve ser contents: read"
  fi
  grep -qE '^concurrency:' "$c" || bad "$n: falta concurrency (cancelar a validação antiga do mesmo PR)"
fi

r="$dir/release.yml"
if [ -f "$r" ]; then
  n=release.yml
  # gatilho: o bloco "on:" tem só push.tags
  on="$(awk '/^on:/ {o=1; next} o && /^[A-Za-z]/ {exit} o {print}' "$r")"
  printf '%s\n' "$on" | grep -qE '^  push:' || bad "$n: gatilho tem de ser push de tag"
  printf '%s\n' "$on" | grep -qE '^    tags:' || bad "$n: push sem tags"
  printf '%s\n' "$on" | grep -qE '^    branches' && bad "$n: push de branch não é permitido"
  printf '%s\n' "$on" | grep -qE '"v\*\.\*\.\*"' || bad "$n: o filtro de tag deve ser v*.*.*"
  [ "$(printf '%s\n' "$on" | grep -cE '^  [A-Za-z_]+:')" = 1 ] || bad "$n: o único gatilho permitido é push (sem pull_request, workflow_dispatch, schedule)"
  grep -qE '^permissions: *\{\}[[:space:]]*$' "$r" || bad "$n: permissions de topo deve ser {}"
  [ "$(grep -cE '^[[:space:]]+contents: *write' "$r")" = 1 ] || bad "$n: 'contents: write' deve aparecer em exatamente um job"
  grep -qE 'permissions: *write-all|id-token: *write|pull-requests: *write' "$r" && bad "$n: permissão ampla demais"
  grep -qE '^  cancel-in-progress: false' "$r" || bad "$n: concurrency deve ter cancel-in-progress: false"
  grep -qE '^  group: .*github.ref' "$r" || bad "$n: concurrency deve ser por tag (github.ref)"
  # segredos: só dentro do job com environment: release
  sec_jobs="$(awk '/^  [A-Za-z0-9_-]+:[[:space:]]*$/ {job=$1} /secrets\.[A-Z_0-9]+/ && !/secrets\.GITHUB_TOKEN/ {print job}' "$r" | sort -u)"
  env_jobs="$(awk '/^  [A-Za-z0-9_-]+:[[:space:]]*$/ {job=$1} /^    environment: release[[:space:]]*$/ {print job}' "$r" | sort -u)"
  [ -z "$sec_jobs" ] || [ "$sec_jobs" = "$env_jobs" ] || bad "$n: segredo usado fora do job com environment: release ($sec_jobs)"
  grep -qE '^[[:space:]]*(echo|printf)[^#]*(P12|P8|PASSWORD|secrets\.)' "$r" && bad "$n: não imprima segredo em log"
  grep -qE 'toolkit version|version \| grep' "$r" || bad "$n: falta a conferência de 'toolkit version'"
  grep -qE 'gh release create .*--draft' "$r" || grep -qE -- '--draft' "$r" || bad "$n: o release tem de sair como rascunho (--draft)"
  grep -qE -- '--verify-tag' "$r" || bad "$n: gh release create sem --verify-tag"
  grep -qE '^[[:space:]]*runs-on: *macos-latest' "$r" && bad "$n: a imagem do macOS do release deve ser FIXA (macos-NN), não macos-latest"
  grep -qE '^[[:space:]]*runs-on: *macos-[0-9]+' "$r" || bad "$n: falta o job macOS com imagem fixa (macos-NN)"
  sg="$(grep -cE 'uses: *actions/setup-go@' "$r" || true)"
  [ "$(grep -cE '^[[:space:]]+cache: false' "$r")" = "$sg" ] || bad "$n: todo setup-go do release precisa de cache: false ($sg)"
fi

if [ "$errs" -gt 0 ]; then echo "check-workflows: $errs problema(s)" >&2; exit 1; fi
echo "workflows ok (${#files[@]} arquivo(s))"
