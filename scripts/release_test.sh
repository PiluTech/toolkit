#!/usr/bin/env bash
# Testa o release.sh sem assinar, notarizar, ler o 1Password nem publicar nada: op, codesign, xcrun e
# ditto são FALSOS (num PATH temporário) e só registram o que receberam. O script roda numa CÓPIA do
# repositório em diretório temporário (git init + commit + tag), porque exige árvore limpa e tag = HEAD.
set -euo pipefail
cd "$(dirname "$0")/.."
src="$PWD"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FALHOU (release): $*" >&2; exit 1; }

# cópia do working tree (com as mudanças ainda não commitadas) num repositório limpo, com a tag v0.0.1 em HEAD
repo="$tmp/repo"; mkdir "$repo"
( cd "$src" && git ls-files -z --cached --others --exclude-standard | while IFS= read -r -d '' f; do [ -e "$f" ] && printf '%s\0' "$f"; done | xargs -0 tar cf - ) | tar xf - -C "$repo"
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
( cd "$repo" && git init -q && git add -A && git commit -q -m copia && git tag v0.0.1 )
script="$repo/scripts/release.sh"

fake="$tmp/fakebin"; log="$tmp/calls.log"
mkdir "$fake"; : >"$log"
cat >"$fake/op" <<EOF
#!/bin/sh
echo "op \$*" >>"$log"
echo "conteudo-secreto-p8"
EOF
cat >"$fake/codesign" <<EOF
#!/bin/sh
echo "codesign \$*" >>"$log"
for last; do :; done
case "\$*" in *--sign*) printf 'ASSINADO' >>"\$last" ;; esac
EOF
cat >"$fake/ditto" <<EOF
#!/bin/sh
echo "ditto \$*" >>"$log"
for last; do :; done
echo zip >"\$last"
EOF
cat >"$fake/xcrun" <<EOF
#!/bin/sh
echo "xcrun \$*" >>"$log"
while [ \$# -gt 0 ]; do
  if [ "\$1" = --key ]; then
    perm="\$(stat -f %Lp "\$2" 2>/dev/null || stat -c %a "\$2")"
    echo "p8perm \$perm \$2" >>"$log"
    echo "p8conteudo \$(cat "\$2")" >>"$log"
  fi
  shift
done
EOF
chmod +x "$fake"/*

export TOOLKIT_RELEASE_ENV="$tmp/nao-existe.env"   # nunca lê o arquivo real do usuário
unset NOTARY_KEY_ID NOTARY_ISSUER NOTARY_P8_OP_REF TOOLKIT_SIGN_IDENTITY
out="$repo/dist"
IDENT="Developer ID Application: Teste da Silva (ABCDE12345)"
rel() { ( cd "$repo" && PATH="$fake:$PATH" "$script" "$@" ); }
calls() { wc -l <"$log" | tr -d ' '; }

# 1. versão inválida
rel --version 0.1 --out "$out" >"$tmp/o" 2>&1 && fail "versão inválida aceita"
rel >"$tmp/o" 2>&1 && fail "sem --version aceito"

# 2. dry-run (inclusive com --sign-macos): nada executa e nada é criado
TOOLKIT_SIGN_IDENTITY="$IDENT" NOTARY_KEY_ID=KID NOTARY_ISSUER=ISS NOTARY_P8_OP_REF=op://x/y rel --version v0.0.1 --out "$out" --dry-run --sign-macos >"$tmp/o" 2>&1 || fail "dry-run falhou: $(cat "$tmp/o")"
[ "$(grep -c 'go build' "$tmp/o")" = 6 ] || fail "dry-run deveria listar 6 builds"
grep -q 'codesign --force --options runtime' "$tmp/o" || fail "dry-run sem passo de codesign"
grep -q 'notarytool submit' "$tmp/o" || fail "dry-run sem passo de notarytool"
[ -e "$out" ] && fail "dry-run criou $out"
[ "$(calls)" = 0 ] || fail "dry-run chamou ferramenta: $(cat "$log")"
grep -q 'conteudo-secreto' "$tmp/o" && fail "dry-run vazou segredo"

# 3. build real sem assinatura
rel --version v0.0.1 --out "$out" >"$tmp/o" 2>&1 || fail "build falhou: $(cat "$tmp/o")"
for f in toolkit_windows_amd64.exe toolkit_windows_arm64.exe toolkit_linux_amd64 toolkit_linux_arm64 \
  toolkit_darwin_amd64 toolkit_darwin_arm64 unix.sh win.ps1 checksums.txt; do
  [ -f "$out/$f" ] || fail "faltou $f em dist"
done
[ "$(wc -l <"$out/checksums.txt" | tr -d ' ')" = 8 ] || fail "checksums.txt deveria ter 8 linhas"
( cd "$out" && if command -v sha256sum >/dev/null; then sha256sum -c checksums.txt; else shasum -a 256 -c checksums.txt; fi ) >/dev/null || fail "checksums.txt não confere"
[ "$(calls)" = 0 ] || fail "sem --sign-macos, ferramenta de assinatura foi chamada: $(cat "$log")"
m="$(uname -m)"; [ "$m" = x86_64 ] && m=amd64; [ "$m" = aarch64 ] && m=arm64
host="toolkit_$(uname -s | tr '[:upper:]' '[:lower:]')_$m"
if [ -x "$out/$host" ]; then
  "$out/$host" version | grep -q 'v0.0.1' || fail "versão não foi embutida no binário"
fi

# 4. --sign-macos sem configuração: falha antes de compilar e sem chamar ferramenta
rm -rf "$out"
TOOLKIT_SIGN_IDENTITY="$IDENT" rel --version v0.0.1 --out "$out" --sign-macos >"$tmp/o" 2>&1 && fail "--sign-macos sem env foi aceito"
grep -q NOTARY "$tmp/o" || fail "mensagem não cita a variável faltante"
[ -e "$out" ] && fail "criou dist antes de validar a configuração"
[ "$(calls)" = 0 ] || fail "chamou ferramenta sem configuração"

# 5. --sign-macos com ferramentas falsas
TOOLKIT_SIGN_IDENTITY="$IDENT" NOTARY_KEY_ID=KID-TESTE NOTARY_ISSUER=ISSUER-TESTE NOTARY_P8_OP_REF=op://cofre/item rel --version v0.0.1 --out "$out" --sign-macos >"$tmp/o" 2>&1 || fail "assinatura (falsa) falhou: $(cat "$tmp/o")"
[ "$(grep -c '^codesign .*--sign' "$log")" = 2 ] || fail "esperava 2 codesign --sign (só macOS): $(cat "$log")"
grep '^codesign' "$log" | grep -qF "$IDENT" || fail "identidade do codesign"
grep '^codesign' "$log" | grep -q -- '--options runtime' || fail "codesign sem hardened runtime"
grep '^codesign' "$log" | grep -qE 'windows|linux' && fail "assinou binário que não é de macOS"
[ "$(grep -c '^xcrun notarytool submit' "$log")" = 2 ] || fail "esperava 2 submissões de notarização"
grep '^xcrun' "$log" | grep -q -- '--key-id KID-TESTE --issuer ISSUER-TESTE --wait' || fail "key-id/issuer não vieram do ambiente"
[ "$(grep -c '^op read op://cofre/item' "$log")" = 1 ] || fail "op read deveria rodar uma vez"
grep -q '^p8perm 600 ' "$log" || fail "p8 temporário não era 600: $(grep p8perm "$log")"
grep -q '^p8conteudo conteudo-secreto-p8' "$log" || fail "p8 não foi gravado no arquivo temporário"
p8path="$(grep -m1 '^p8perm' "$log" | cut -d' ' -f3)"
[ -e "$p8path" ] && fail "p8 temporário não foi apagado"
grep -q 'conteudo-secreto' "$tmp/o" && fail "segredo apareceu na saída"
# o checksums.txt é calculado DEPOIS da assinatura
( cd "$out" && if command -v sha256sum >/dev/null; then sha256sum -c checksums.txt; else shasum -a 256 -c checksums.txt; fi ) >/dev/null || fail "checksums não confere com binários assinados"
grep -q ASSINADO "$out/toolkit_darwin_arm64" || fail "binário darwin não foi assinado"
grep -q ASSINADO "$out/toolkit_linux_amd64" && fail "binário linux foi 'assinado'"

# 5b. a identidade é obrigatória e não tem valor padrão no repositório; release.env com permissão aberta é recusado
rm -rf "$out"
NOTARY_KEY_ID=K NOTARY_ISSUER=I NOTARY_P8_OP_REF=op://x/y rel --version v0.0.1 --out "$out" --sign-macos >"$tmp/o" 2>&1 && fail "--sign-macos sem identidade aceito"
grep -q TOOLKIT_SIGN_IDENTITY "$tmp/o" || fail "mensagem não cita TOOLKIT_SIGN_IDENTITY"
printf 'NOTARY_KEY_ID=K\nNOTARY_ISSUER=I\nNOTARY_P8_OP_REF=op://x/y\nTOOLKIT_SIGN_IDENTITY=x\n' >"$tmp/aberto.env"; chmod 644 "$tmp/aberto.env"
TOOLKIT_RELEASE_ENV="$tmp/aberto.env" rel --version v0.0.1 --out "$out" --sign-macos >"$tmp/o" 2>&1 && fail "release.env 644 aceito"
grep -q 'permissão 600' "$tmp/o" || fail "mensagem do modo do release.env: $(cat "$tmp/o")"
chmod 600 "$tmp/aberto.env"
TOOLKIT_RELEASE_ENV="$tmp/aberto.env" rel --version v0.0.1 --out "$out" --sign-macos --dry-run >"$tmp/o" 2>&1 || fail "release.env 600 recusado: $(cat "$tmp/o")"

# 5c. estado do git: árvore suja, tag ausente ou fora de HEAD recusam ANTES de compilar/apagar
rm -rf "$out"; mkdir "$out"; echo guarda >"$out/marca"
echo sujo >"$repo/sujo.txt"
rel --version v0.0.1 --out "$out" >"$tmp/o" 2>&1 && fail "árvore suja aceita"
grep -q 'árvore suja' "$tmp/o" || fail "mensagem da árvore suja: $(cat "$tmp/o")"
rm "$repo/sujo.txt"
rel --version v0.0.2 --out "$out" >"$tmp/o" 2>&1 && fail "tag ausente aceita"
grep -q 'não existe' "$tmp/o" || fail "mensagem da tag ausente: $(cat "$tmp/o")"
( cd "$repo" && git commit -q --allow-empty -m novo )
rel --version v0.0.1 --out "$out" >"$tmp/o" 2>&1 && fail "tag fora de HEAD aceita"
grep -q 'não aponta para HEAD' "$tmp/o" || fail "mensagem da tag fora de HEAD: $(cat "$tmp/o")"
[ -f "$out/marca" ] || fail "apagou o --out antes de validar o git"
( cd "$repo" && git tag -f v0.0.1 >/dev/null )   # volta a tag para HEAD

# 5d. --out só dentro do repositório, nunca raiz, repo, .git, link, vazio ou diretório versionado
mkdir "$tmp/fora"; echo guarda >"$tmp/fora/marca"
ln -s "$tmp/fora" "$repo/elo"
for bad in "$tmp/fora" "$repo" "$repo/" / "" . .. "$repo/.git" "$repo/elo" "$repo/scripts" "$repo/dist/../.." ; do
  rel --version v0.0.1 --out "$bad" >"$tmp/o" 2>&1 && fail "--out '$bad' aceito"
  if [ ! -f "$repo/scripts/release.sh" ] || [ ! -f "$tmp/fora/marca" ] || [ ! -d "$repo/.git" ]; then fail "--out '$bad' apagou algo"; fi
done
rm -f "$repo/elo"
rel --version v0.0.1 --out "$out" --dry-run >"$tmp/o" 2>&1 || fail "--out legítimo recusado: $(cat "$tmp/o")"
rm -rf "$out"

# 5e. toolchain: o go.mod exige go X; com um go mais velho fixo (GOTOOLCHAIN=local falso) o release recusa
mkdir -p "$tmp/oldgo"
cat >"$tmp/oldgo/go" <<EOF2
#!/bin/sh
case "\$1 \$2" in "env GOVERSION") echo go1.20.0 ;; *) exec "$(command -v go)" "\$@" ;; esac
EOF2
chmod +x "$tmp/oldgo/go"
rm -rf "$out"
( cd "$repo" && PATH="$tmp/oldgo:$fake:$PATH" "$script" --version v0.0.1 --out "$out" ) >"$tmp/o" 2>&1 && fail "toolchain velha aceita"
grep -q 'mais velha que a do go.mod' "$tmp/o" || fail "mensagem da toolchain: $(cat "$tmp/o")"
[ -e "$out" ] && fail "criou dist antes de validar a toolchain"
rel --version v0.0.1 --out "$out" --dry-run >"$tmp/o" 2>&1 || fail "toolchain atual recusada: $(cat "$tmp/o")"
grep -q '^toolchain: go' "$tmp/o" || fail "o release não informa a toolchain"

# 6. nada de segredo nem de caminho do 1Password no repositório, e sem publicação automática
if grep -nE 'op://|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' "$script"; then fail "release.sh tem credencial/caminho literal"; fi
grep -nE '^[^#]*gh +release' "$script" | grep -v echo && fail "release.sh publica sozinho"
grep -n 'Paulo\|WU7Z54BRYQ' "$script" && fail "release.sh tem a identidade de assinatura literal"

echo "ok release_test"
