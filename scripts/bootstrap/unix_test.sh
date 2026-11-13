#!/usr/bin/env bash
# Testa o unix.sh sem rede e sem instalar nada fora de um diretório temporário: release falsa em
# file:// e num servidor HTTP local, checksums adulterados (deve falhar e não instalar), dry-run.
set -euo pipefail
cd "$(dirname "$0")"
script="$PWD/unix.sh"

tmp="$(mktemp -d)"
srv=""
cleanup() {
  if [ -n "$srv" ]; then kill "$srv" 2>/dev/null || true; wait "$srv" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() { echo "FALHOU: $*" >&2; exit 1; }

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "SO sem teste"; exit 0 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; arm64 | aarch64) arch=arm64 ;; *) echo "arch sem teste"; exit 0 ;; esac
asset="toolkit_${os}_${arch}"

hash_of() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

rel="$tmp/release"
mkdir -p "$rel"
printf '#!/bin/sh\necho toolkit-falso\n' >"$rel/$asset"
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"

# run <base> [args...]: roda o bootstrap com HOME e destino isolados; devolve o status.
run() {
  local base="$1"; shift
  rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
  HOME="$tmp/home" TOOLKIT_NO_LAUNCH=1 TOOLKIT_RELEASE_BASE="$base" sh "$script" "$@" >"$tmp/out" 2>&1
}
installed() { [ -x "$tmp/home/.local/bin/toolkit" ]; }

# 1. caminho feliz
run "file://$rel" || fail "instalação válida falhou: $(cat "$tmp/out")"
installed || fail "binário não foi instalado em ~/.local/bin"
[ "$("$tmp/home/.local/bin/toolkit")" = "toolkit-falso" ] || fail "binário instalado não é o baixado"
grep -q "SHA-256 conferido" "$tmp/out" || fail "faltou a confirmação do hash"

# 2. hash adulterado: aborta e não instala
printf '%s  %s\n' "$(printf 'x' | { sha256sum 2>/dev/null || shasum -a 256; } | cut -d' ' -f1)" "$asset" >"$rel/checksums.txt"
run "file://$rel" && fail "checksums adulterado foi aceito"
installed && fail "instalou mesmo com hash divergente"
grep -q "diverge" "$tmp/out" || fail "mensagem de divergência ausente: $(cat "$tmp/out")"

# 3. binário adulterado depois de gerar o checksums
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"
printf 'malicioso' >>"$rel/$asset"
run "file://$rel" && fail "binário adulterado foi aceito"
installed && fail "instalou binário adulterado"
printf '#!/bin/sh\necho toolkit-falso\n' >"$rel/$asset"

# 4. asset ausente do checksums.txt
printf '%s  outro_arquivo\n' "$(hash_of "$rel/$asset")" >"$rel/checksums.txt"
run "file://$rel" && fail "asset fora do checksums.txt foi aceito"
installed && fail "instalou sem entrada no checksums.txt"
grep -q "não lista" "$tmp/out" || fail "mensagem de asset ausente: $(cat "$tmp/out")"

# 5. checksums.txt inexistente
rm "$rel/checksums.txt"
run "file://$rel" && fail "sem checksums.txt foi aceito"
installed && fail "instalou sem checksums.txt"
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"

# 6. dry-run não baixa nem instala (base inexistente prova que não houve download)
run "file://$tmp/nao-existe" --dry-run || fail "dry-run falhou: $(cat "$tmp/out")"
installed && fail "dry-run instalou"
grep -q "dry-run" "$tmp/out" || fail "dry-run sem saída"

# 7. argumento desconhecido e versão inválida
run "file://$rel" --nao-existe && fail "argumento desconhecido foi aceito"
rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
if HOME="$tmp/home" TOOLKIT_VERSION='v1/../x' sh "$script" --dry-run >"$tmp/out" 2>&1; then fail "versão inválida aceita"; fi
HOME="$tmp/home" TOOLKIT_VERSION='0.1.0' sh "$script" --dry-run >"$tmp/out" 2>&1 || fail "versão 0.1.0 recusada"
grep -q "releases/download/v0.1.0" "$tmp/out" || fail "versão sem v não foi normalizada: $(cat "$tmp/out")"
HOME="$tmp/home" sh "$script" --dry-run >"$tmp/out" 2>&1 || fail "dry-run padrão falhou"
grep -q "releases/latest/download" "$tmp/out" || fail "URL padrão não é latest"
grep -q "github.com/PiluTech/toolkit" "$tmp/out" || fail "repositório errado"

# 8. SO não suportado (uname falso no PATH)
mkdir "$tmp/fakebin"
printf '#!/bin/sh\necho Plan9\n' >"$tmp/fakebin/uname"; chmod +x "$tmp/fakebin/uname"
if PATH="$tmp/fakebin:$PATH" HOME="$tmp/home" sh "$script" --dry-run >"$tmp/out" 2>&1; then fail "SO não suportado aceito"; fi

# 9. servidor HTTP local (quando há python3): caminho feliz e hash adulterado
if command -v python3 >/dev/null 2>&1; then
  port=$((20000 + RANDOM % 20000))
  (cd "$rel" && exec python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1) &
  srv=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do curl -fs "http://127.0.0.1:$port/checksums.txt" >/dev/null 2>&1 && break; sleep 0.3; done
  run "http://127.0.0.1:$port" || fail "instalação via HTTP falhou: $(cat "$tmp/out")"
  installed || fail "HTTP: não instalou"
  printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$asset" >"$rel/checksums.txt"
  run "http://127.0.0.1:$port" && fail "HTTP: hash adulterado aceito"
  installed && fail "HTTP: instalou com hash adulterado"
fi

# 10. download cortado no meio (curl | sh): nenhum pedaço do script executa e nada é instalado
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"
size=$(wc -c <"$script" | tr -d ' ')
for n in 200 700 1200 1800 $((size / 2)) $((size - 30)); do
  rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
  head -c "$n" "$script" | HOME="$tmp/home" TOOLKIT_RELEASE_BASE="file://$rel" sh >"$tmp/out" 2>&1 || true
  installed && fail "script cortado em $n bytes instalou algo"
  [ -z "$(ls -A "$tmp/home")" ] || fail "script cortado em $n bytes criou arquivos em HOME"
done
[ "$(tail -n1 "$script")" = 'main "$@"' ] || fail "a última linha do unix.sh deve ser 'main \"\$@\"'"

# 11. política de download: curl só HTTPS (nos testes locais libera http e file), TLS 1.2+
grep -q -- "--proto \"\$proto\" --proto-redir \"\$proto\" --tlsv1.2" "$script" || fail "curl sem --proto/--tlsv1.2"
grep -q "proto='=https'" "$script" || fail "política padrão não é só https"
mkdir "$tmp/curlbin"
printf '#!/bin/sh\necho "$*" >>"%s/curl.log"\nexec %s "$@"\n' "$tmp" "$(command -v curl)" >"$tmp/curlbin/curl"; chmod +x "$tmp/curlbin/curl"
: >"$tmp/curl.log"; rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
PATH="$tmp/curlbin:$PATH" HOME="$tmp/home" TOOLKIT_NO_LAUNCH=1 TOOLKIT_RELEASE_BASE="file://$rel" sh "$script" >"$tmp/out" 2>&1 || fail "instalação com curl espião falhou"
grep -q -- '--proto =https,http,file' "$tmp/curl.log" || fail "curl sem --proto: $(cat "$tmp/curl.log")"

# 12. SIGINT/SIGTERM durante o download: aborta (não segue para a instalação) e limpa o temporário
# shellcheck disable=SC2016  # $PPID deve expandir no curl falso, não aqui
printf '#!/bin/sh\nkill -TERM $PPID\nsleep 5\n' >"$tmp/curlbin/curl"; chmod +x "$tmp/curlbin/curl"
rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"
if PATH="$tmp/curlbin:$PATH" HOME="$tmp/home" TOOLKIT_NO_LAUNCH=1 TOOLKIT_RELEASE_BASE="file://$rel" sh "$script" >"$tmp/out" 2>&1; then fail "SIGTERM não abortou o bootstrap"; fi
installed && fail "instalou após SIGTERM"

# 13. abre a TUI depois de instalar: só com terminal real, nunca em CI nem com opt-out.
# /dev/tty falso por TEST_TTY (arquivo comum); o binário falso registra os argumentos e se a stdin era o tty.
mkdir -p "$tmp/launch"
cat >"$rel/$asset" <<'FAKE'
#!/bin/sh
echo "args=$*" >>"$TOOLKIT_TEST_LOG"
[ -z "${TOOLKIT_TEST_FAIL:-}" ] || exit 7
FAKE
printf '%s  %s\n' "$(hash_of "$rel/$asset")" "$asset" >"$rel/checksums.txt"
fake_tty="$tmp/launch/tty"; : >"$fake_tty"
# O script distribuído NÃO tem ganchos de teste: os fatos (stdout/stdin/tty) entram numa CÓPIA dele, que troca
# stdout_is_tty, stdin_kind e o caminho do tty antes da última linha (main "$@").
hooked="$tmp/launch/unix_hooked.sh"
[ "$(tail -n 1 "$script")" = 'main "$@"' ] || fail "a última linha do unix.sh deveria ser main \"\$@\""
grep -q '^  tty="/dev/tty"$' "$script" || fail "unix.sh: a linha tty=\"/dev/tty\" mudou (o harness precisa dela)"
# shellcheck disable=SC2016  # a ${TEST_TTY} entra literal na cópia, não é para expandir aqui
sed -e 's#^  tty="/dev/tty"$#  tty="${TEST_TTY}"#' -e '$d' "$script" >"$hooked"
cat >>"$hooked" <<'HOOKS'
stdout_is_tty() { [ "${TEST_STDOUT:-}" = tty ]; }
stdin_kind() { echo "${TEST_STDIN:-file}"; }
main "$@"
HOOKS
grep -q 'TEST_TTY' "$hooked" || fail "a cópia com ganchos não foi gerada"
log="$tmp/launch/log"
# launch [VAR=valor...] -- [args]: sem variáveis de CI herdadas; devolve o status do bootstrap.
launch() {
  local envs=()
  while [ "$1" != "--" ]; do envs+=("$1"); shift; done
  shift
  rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"; : >"$log"
  env -u CI -u GITHUB_ACTIONS -u TF_BUILD -u TOOLKIT_NO_LAUNCH -u GITLAB_CI -u BUILDKITE -u JENKINS_URL \
    -u CIRCLECI -u TRAVIS -u TEAMCITY_VERSION -u CODEBUILD_BUILD_ID -u APPVEYOR \
    HOME="$tmp/home" TOOLKIT_RELEASE_BASE="file://$rel" TOOLKIT_TEST_LOG="$log" \
    TEST_STDOUT=tty TEST_STDIN=pipe "${envs[@]}" \
    sh "$hooked" "$@" >"$tmp/out" 2>&1
}
opened() { grep -q '^args=tui$' "$log"; }

launch TEST_TTY="$fake_tty" -- || fail "instalação com tty falhou: $(cat "$tmp/out")"
installed || fail "não instalou (launch)"
opened || fail "não abriu a TUI com tty disponível: $(cat "$tmp/out")"
grep -q "SHA-256 conferido" "$tmp/out" || fail "faltou a mensagem de instalação"

launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH=1 -- || fail "NO_LAUNCH=1 falhou"
installed || fail "NO_LAUNCH=1 não instalou"
opened && fail "abriu a TUI com TOOLKIT_NO_LAUNCH=1"
launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH=0 -- || fail "NO_LAUNCH=0 falhou"
opened || fail "TOOLKIT_NO_LAUNCH=0 deveria abrir"
launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH=false -- || fail "NO_LAUNCH=false falhou"
opened || fail "TOOLKIT_NO_LAUNCH=false deveria abrir"
launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH= -- || fail "NO_LAUNCH vazio falhou"
opened || fail "TOOLKIT_NO_LAUNCH vazio deveria abrir"

launch TEST_TTY="$fake_tty" -- --no-launch || fail "--no-launch falhou"
installed || fail "--no-launch não instalou"
opened && fail "abriu a TUI com --no-launch"

for ci in CI=true GITHUB_ACTIONS=true TF_BUILD=True GITLAB_CI=true BUILDKITE=true JENKINS_URL=https://ci.example CIRCLECI=true; do
  launch TEST_TTY="$fake_tty" "$ci" -- || fail "$ci: instalação falhou"
  installed || fail "$ci: não instalou"
  opened && fail "abriu a TUI com $ci"
done

launch TEST_TTY="$tmp/launch/nao-existe" -- || fail "sem tty: bootstrap falhou: $(cat "$tmp/out")"
installed || fail "sem tty: não instalou"
opened && fail "abriu a TUI sem tty"
grep -q "toolkit tui" "$tmp/out" || fail "sem tty: faltou a dica de como abrir depois: $(cat "$tmp/out")"

# stdout/stdin: o pipe do próprio curl | sh é a exceção atendida por /dev/tty; saída redirecionada (> log, | tee) ou
# entrada de arquivo (sh unix.sh < arquivo) NÃO abre, mesmo com tty disponível: instala e só diz como abrir depois.
launch TEST_TTY="$fake_tty" TEST_STDIN=pipe -- || fail "stdin pipe: falhou"
opened || fail "stdin pipe (curl | sh) com tty deveria abrir"
launch TEST_TTY="$fake_tty" TEST_STDIN=tty -- || fail "stdin tty: falhou"
opened || fail "stdin terminal deveria abrir"
launch TEST_TTY="$fake_tty" TEST_STDOUT=notty -- || fail "stdout redirecionado: falhou"
installed || fail "stdout redirecionado: não instalou"
opened && fail "abriu a TUI com a saída redirecionada (> log) mesmo com tty disponível"
grep -q "toolkit tui" "$tmp/out" || fail "stdout redirecionado: faltou a dica"
launch TEST_TTY="$fake_tty" TEST_STDIN=file -- || fail "stdin arquivo: falhou"
installed || fail "stdin arquivo: não instalou"
opened && fail "abriu a TUI com a entrada de arquivo redirecionado"
launch TEST_TTY="$fake_tty" TEST_STDOUT=notty TEST_STDIN=file -- || fail "ambos redirecionados: falhou"
opened && fail "abriu a TUI com entrada e saída redirecionadas"

# O script distribuído ignora variáveis de teste herdadas do ambiente: com TEST_*/TOOLKIT_TEST_* exportadas e a
# detecção REAL (saída em arquivo, entrada de arquivo), nada abre e o binário nunca é chamado com "tui".
rm -rf "${tmp:?}/home"; mkdir -p "$tmp/home"; : >"$log"
env -u CI -u GITHUB_ACTIONS -u TF_BUILD -u TOOLKIT_NO_LAUNCH -u GITLAB_CI -u BUILDKITE -u JENKINS_URL \
  -u CIRCLECI -u TRAVIS -u TEAMCITY_VERSION -u CODEBUILD_BUILD_ID -u APPVEYOR \
  HOME="$tmp/home" TOOLKIT_RELEASE_BASE="file://$rel" TOOLKIT_TEST_LOG="$log" \
  TEST_STDOUT=tty TEST_STDIN=pipe TEST_TTY="$fake_tty" TOOLKIT_TEST_STDOUT=tty TOOLKIT_TEST_STDIN=pipe TOOLKIT_TTY="$fake_tty" \
  sh "$script" </dev/null >"$tmp/out" 2>&1 || fail "release normal com variáveis de teste herdadas falhou"
installed || fail "variáveis herdadas: não instalou"
opened && fail "variáveis de teste herdadas forçaram a abertura da TUI no script distribuído"
grep -qE 'TEST_STD|TOOLKIT_TTY|TOOLKIT_TEST_' "$script" && fail "o unix.sh distribuído não pode ler ganchos de teste"

# semântica do envOn: espaços, caixa, no e off também desligam a variável (TOOLKIT_NO_LAUNCH/CI "desligados" abrem)
for v in " 0 " NO off OFF " false" "false " "	off	" "  No  "; do
  launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH="$v" -- || fail "NO_LAUNCH='$v' falhou"
  opened || fail "TOOLKIT_NO_LAUNCH='$v' (desligado pelo envOn) deveria abrir"
  launch TEST_TTY="$fake_tty" CI="$v" -- || fail "CI='$v' falhou"
  opened || fail "CI='$v' (desligado pelo envOn) deveria abrir"
done

# trim só nas pontas: espaço/tab INTERNO não desliga ("f alse", "o ff" e "0 0" continuam ligados = recusam abrir)
for v in "f alse" "o ff" "0 0" "n	o"; do
  launch TEST_TTY="$fake_tty" TOOLKIT_NO_LAUNCH="$v" -- || fail "NO_LAUNCH='$v' falhou"
  opened && fail "TOOLKIT_NO_LAUNCH='$v' (valor ligado pelo envOn) deveria recusar abrir"
  launch TEST_TTY="$fake_tty" CI="$v" -- || fail "CI='$v' falhou"
  opened && fail "CI='$v' (valor ligado pelo envOn) deveria recusar abrir"
done

# falha da TUI não derruba o bootstrap: exit 0, mensagem em pt-BR, mensagem de instalação antes
launch TEST_TTY="$fake_tty" TOOLKIT_TEST_FAIL=1 -- || fail "falha da TUI derrubou o bootstrap: $(cat "$tmp/out")"
installed || fail "falha da TUI: binário não instalado"
opened || fail "falha da TUI: não tentou abrir"
grep -q "toolkit tui" "$tmp/out" || fail "falha da TUI: faltou a dica de como abrir depois: $(cat "$tmp/out")"
[ "$(grep -n 'SHA-256 conferido' "$tmp/out" | head -n1 | cut -d: -f1)" -lt "$(grep -n 'toolkit tui' "$tmp/out" | head -n1 | cut -d: -f1)" ] || fail "mensagem de instalação deve vir antes"

# dry-run nunca abre
launch TEST_TTY="$fake_tty" -- --dry-run || fail "dry-run falhou"
opened && fail "dry-run abriu a TUI"

echo "ok unix_test"
