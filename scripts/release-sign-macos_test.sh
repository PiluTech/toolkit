#!/usr/bin/env bash
# Testa o release-sign-macos.sh com ferramentas FALSAS (security, codesign, xcrun, ditto num PATH temporário):
# nada é assinado, notarizado nem importado de verdade, e nenhum segredo real existe aqui.
set -euo pipefail
cd "$(dirname "$0")/.."
src="$PWD"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FALHOU (release-sign-macos): $*" >&2; exit 1; }

fake="$tmp/fakebin"; log="$tmp/calls.log"; mkdir "$fake"; : >"$log"
cat >"$fake/security" <<EOF
#!/bin/sh
echo "security \$*" >>"$log"
case "\$1" in
  list-keychains) case "\$*" in *" -s "*) ;; *) printf '    "/fake/login.keychain-db"\n' ;; esac ;;
  create-keychain) shift; while [ \$# -gt 0 ]; do [ "\$1" = -p ] && echo "kcpass \$2" >>"$log"; last="\$1"; shift; done; echo kc >"\$last"; echo "kcpath \$last" >>"$log" ;;
  import) echo "p12conteudo \$(cat "\$2")" >>"$log"; echo "p12perm \$(stat -f %Lp "\$2" 2>/dev/null || stat -c %a "\$2") \$2" >>"$log"
          [ -n "\$FAKE_IMPORT_FAIL" ] && exit 1 ;;
  find-identity) printf '  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Developer ID Application: Teste (ABCDE12345)"\n     1 valid identities found\n' ;;
  delete-keychain) echo "deleted \$2" >>"$log"; rm -f "\$2" ;;
esac
exit 0
EOF
cat >"$fake/codesign" <<EOF
#!/bin/sh
echo "codesign \$*" >>"$log"
[ -n "\$FAKE_CODESIGN_FAIL" ] && exit 1
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
    echo "p8perm \$(stat -f %Lp "\$2" 2>/dev/null || stat -c %a "\$2") \$2" >>"$log"
    echo "p8conteudo \$(cat "\$2")" >>"$log"
  fi
  shift
done
echo "  status: \${FAKE_NOTARY_STATUS:-Accepted}"
EOF
chmod +x "$fake"/*

mkdist() {
  rm -rf "$tmp/dist"; mkdir "$tmp/dist"
  for f in toolkit_darwin_amd64 toolkit_darwin_arm64 toolkit_linux_amd64 toolkit_linux_arm64 toolkit_windows_amd64.exe unix.sh win.ps1; do echo "bin-$f" >"$tmp/dist/$f"; done
  scripts/release-checksums.sh "$tmp/dist"
}
DEVELOPER_ID_P12_BASE64="$(printf 'P12-SECRETO-FALSO' | base64)"
NOTARY_P8_BASE64="$(printf 'P8-SECRETO-FALSO' | base64)"
export DEVELOPER_ID_P12_BASE64 NOTARY_P8_BASE64
export DEVELOPER_ID_P12_PASSWORD="senha-p12-falsa"
export NOTARY_KEY_ID="KEYID-FALSO" NOTARY_ISSUER="ISSUER-FALSO"
sign() { PATH="$fake:$PATH" "$src/scripts/release-sign-macos.sh" "$@"; }

# 1. argumentos e ambiente: recusa sem tocar em ferramenta
mkdist; : >"$log"
sign >"$tmp/o" 2>&1 && fail "sem --dir aceito"
sign --dir "$tmp/nao-existe" >"$tmp/o" 2>&1 && fail "--dir inexistente aceito"
for v in DEVELOPER_ID_P12_BASE64 DEVELOPER_ID_P12_PASSWORD NOTARY_KEY_ID NOTARY_ISSUER NOTARY_P8_BASE64; do
  ( unset "$v"; sign --dir "$tmp/dist" >"$tmp/o" 2>&1 ) && fail "sem $v aceito"
  grep -q "$v" "$tmp/o" || fail "mensagem não cita $v"
done
rm "$tmp/dist/toolkit_darwin_arm64"
sign --dir "$tmp/dist" >"$tmp/o" 2>&1 && fail "binário ausente aceito"
[ ! -s "$log" ] || fail "tocou em ferramenta antes de validar: $(cat "$log")"

# 2. caminho feliz
mkdist; : >"$log"
sign --dir "$tmp/dist" >"$tmp/o" 2>&1 || fail "assinatura (falsa) falhou: $(cat "$tmp/o")"
[ "$(grep -c '^codesign .*--sign 0123456789ABCDEF' "$log")" = 2 ] || fail "esperava 2 codesign --sign com o hash da identidade: $(cat "$log")"
grep '^codesign' "$log" | grep -q -- '--options runtime' || fail "sem hardened runtime"
grep '^codesign' "$log" | grep -q -- '--keychain ' || fail "codesign sem a keychain temporária"
grep '^codesign' "$log" | grep -qE 'linux|windows|unix.sh|win.ps1' && fail "assinou arquivo que não é de macOS"
[ "$(grep -c '^xcrun notarytool submit' "$log")" = 2 ] || fail "esperava 2 submissões"
grep '^xcrun' "$log" | grep -q -- '--key-id KEYID-FALSO --issuer ISSUER-FALSO --wait' || fail "key-id/issuer"
grep -q '^p12conteudo P12-SECRETO-FALSO' "$log" || fail ".p12 não foi decodificado do base64"
grep -q '^p12perm 600 ' "$log" || fail ".p12 temporário não era 600"
grep -q '^p8conteudo P8-SECRETO-FALSO' "$log" || fail ".p8 não foi decodificado do base64"
grep -q '^p8perm 600 ' "$log" || fail ".p8 temporário não era 600"
p8path="$(grep -m1 '^p8perm' "$log" | cut -d' ' -f3)"; [ -e "$p8path" ] && fail ".p8 temporário não foi apagado"
p12path="$(grep -m1 '^p12perm' "$log" | cut -d' ' -f3)"; [ -e "$p12path" ] && fail ".p12 temporário não foi apagado"
kcpath="$(grep -m1 '^kcpath' "$log" | cut -d' ' -f2)"; [ -e "$kcpath" ] && fail "keychain temporária não foi apagada"
grep -q "^deleted $kcpath" "$log" || fail "delete-keychain não foi chamado"
grep -q '^security list-keychains -d user -s /fake/login.keychain-db' "$log" || fail "a lista original de keychains não foi restaurada: $(grep list-keychains "$log")"
for s in P12-SECRETO-FALSO P8-SECRETO-FALSO senha-p12-falsa "$DEVELOPER_ID_P12_BASE64" "$NOTARY_P8_BASE64"; do
  grep -qF "$s" "$tmp/o" && fail "segredo apareceu na saída"
done
grep -q ASSINADO "$tmp/dist/toolkit_darwin_arm64" || fail "darwin arm64 não assinado"
grep -q ASSINADO "$tmp/dist/toolkit_linux_amd64" && fail "linux foi 'assinado'"
[ "$(wc -l <"$tmp/dist/checksums.txt" | tr -d ' ')" = 7 ] || fail "checksums.txt deveria ter 7 linhas"
( cd "$tmp/dist" && if command -v sha256sum >/dev/null; then sha256sum -c checksums.txt; else shasum -a 256 -c checksums.txt; fi ) >/dev/null || fail "checksums não confere com os binários assinados"

# 3. falhas: a keychain e os temporários são apagados mesmo assim, e a saída não vaza segredo
for mode in FAKE_CODESIGN_FAIL FAKE_IMPORT_FAIL; do
  mkdist; : >"$log"
  ( export "$mode=1"; sign --dir "$tmp/dist" ) >"$tmp/o" 2>&1 && fail "$mode: falha engolida"
  kcpath="$(grep -m1 '^kcpath' "$log" | cut -d' ' -f2)"
  [ -n "$kcpath" ] && [ ! -e "$kcpath" ] || fail "$mode: keychain não foi apagada"
  grep -q "^deleted $kcpath" "$log" || fail "$mode: sem delete-keychain"
  grep -qF 'senha-p12-falsa' "$tmp/o" && fail "$mode: senha na saída"
done
mkdist; : >"$log"
( export FAKE_NOTARY_STATUS=Invalid; sign --dir "$tmp/dist" ) >"$tmp/o" 2>&1 && fail "notarização rejeitada foi aceita"
grep -q 'não foi aceita' "$tmp/o" || fail "mensagem da notarização rejeitada"
# sem checksums novos quando a notarização falha: o checksums.txt antigo não pode ser dado como final
mkdist; : >"$log"
( export DEVELOPER_ID_P12_BASE64='%%%nao-base64%%%'; sign --dir "$tmp/dist" ) >"$tmp/o" 2>&1 && fail "base64 inválido aceito"

# 4. nada de segredo literal nem de publicação no script
if grep -nE 'op://|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' scripts/release-sign-macos.sh; then fail "credencial literal no script"; fi
grep -nE '^[^#]*gh +release|^[^#]*echo[^#]*(P12|P8|PASS)' scripts/release-sign-macos.sh && fail "o script publica ou imprime segredo"

echo "ok release-sign-macos_test"
