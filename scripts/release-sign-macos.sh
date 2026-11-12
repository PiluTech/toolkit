#!/usr/bin/env bash
# Assina (codesign, Developer ID) e notariza os 2 binários de macOS de uma pasta de release e recalcula o
# checksums.txt DEPOIS da assinatura. Feito para o job macOS da pipeline (.github/workflows/release.yml), que
# recebe os segredos do environment "release" por variáveis de ambiente:
#
#   DEVELOPER_ID_P12_BASE64    certificado Developer ID Application (.p12) em base64
#   DEVELOPER_ID_P12_PASSWORD  senha do .p12
#   NOTARY_KEY_ID              id da chave de API do notarytool
#   NOTARY_ISSUER              issuer da chave de API
#   NOTARY_P8_BASE64           chave de API (.p8) em base64
#
#   scripts/release-sign-macos.sh --dir dist
#
# Nada é impresso (nem em erro). Tudo temporário (.p12, .p8, zips, keychain) fica em um diretório mktemp 700,
# a keychain é criada só para este processo, e o trap apaga tudo e restaura a lista de keychains, mesmo em falha.
set -euo pipefail
cd "$(dirname "$0")/.."

DIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) DIR="${2:?--dir precisa de valor}"; shift 2 ;;
    -h | --help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "release-sign-macos: argumento desconhecido: $1" >&2; exit 2 ;;
  esac
done
die() { echo "release-sign-macos: $*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }

[ -n "$DIR" ] && [ -d "$DIR" ] || die "use --dir <pasta com os binários> (a pasta tem de existir)"
BINS=(toolkit_darwin_amd64 toolkit_darwin_arm64)
for a in "${BINS[@]}"; do [ -f "$DIR/$a" ] || die "faltou $DIR/$a"; done
for n in DEVELOPER_ID_P12_BASE64 DEVELOPER_ID_P12_PASSWORD NOTARY_KEY_ID NOTARY_ISSUER NOTARY_P8_BASE64; do
  [ -n "${!n:-}" ] || die "variável de ambiente $n vazia ou ausente (segredo do environment release)"
done
for c in security codesign xcrun ditto openssl base64; do command -v "$c" >/dev/null 2>&1 || die "'$c' não encontrado (este script roda em macOS)"; done

# Valores sensíveis nunca aparecem no log do Actions.
if [ -n "${GITHUB_ACTIONS:-}" ]; then
  for n in DEVELOPER_ID_P12_PASSWORD NOTARY_P8_BASE64 DEVELOPER_ID_P12_BASE64; do echo "::add-mask::${!n}"; done
fi

work="$(umask 077; mktemp -d "${TMPDIR:-/tmp}/toolkit-sign.XXXXXX")"
kc="$work/toolkit-release.keychain-db"
orig_keychains=()
kc_created=0
cleanup() {
  set +e
  if [ "${#orig_keychains[@]}" -gt 0 ]; then security list-keychains -d user -s "${orig_keychains[@]}" >/dev/null 2>&1; fi
  [ "$kc_created" = 1 ] && security delete-keychain "$kc" >/dev/null 2>&1
  rm -rf "$work"
}
# Sinal (cancelamento do job): sair com erro; só o trap EXIT limpa. Um trap de INT/TERM que só limpasse deixaria
# o script SEGUIR (sem keychain, com "set +e") e terminar com sucesso.
trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT

step "keychain temporária e certificado Developer ID"
kcpass="$(openssl rand -hex 24)"
[ -n "${GITHUB_ACTIONS:-}" ] && echo "::add-mask::$kcpass"
p12="$work/cert.p12"
printf '%s' "$DEVELOPER_ID_P12_BASE64" | base64 --decode >"$p12" 2>/dev/null || die "DEVELOPER_ID_P12_BASE64 não é base64 válido"
chmod 600 "$p12"
while IFS= read -r line; do
  line="${line#"${line%%[![:space:]]*}"}"; line="${line//\"/}"
  [ -n "$line" ] && orig_keychains+=("$line")
done < <(security list-keychains -d user)
security create-keychain -p "$kcpass" "$kc" >/dev/null
kc_created=1
security set-keychain-settings -lut 3600 "$kc" >/dev/null
security unlock-keychain -p "$kcpass" "$kc" >/dev/null
security import "$p12" -P "$DEVELOPER_ID_P12_PASSWORD" -A -t cert -f pkcs12 -k "$kc" >/dev/null 2>&1 || die "não consegui importar o .p12 (senha errada ou arquivo inválido)"
rm -f "$p12"
security set-key-partition-list -S apple-tool:,apple: -s -k "$kcpass" "$kc" >/dev/null 2>&1 || die "set-key-partition-list falhou"
# A keychain temporária entra na busca do codesign sem tirar as existentes.
security list-keychains -d user -s "$kc" ${orig_keychains[@]+"${orig_keychains[@]}"} >/dev/null

identity="$(security find-identity -v -p codesigning "$kc" | awk '/Developer ID Application/ { print $2; exit }')"
[ -n "$identity" ] || die "nenhuma identidade 'Developer ID Application' no .p12"

step "assinatura (codesign, hardened runtime)"
for a in "${BINS[@]}"; do
  codesign --force --options runtime --timestamp --keychain "$kc" --sign "$identity" "$DIR/$a"
  codesign --verify --strict --verbose=2 "$DIR/$a"
done

step "notarização (notarytool)"
p8="$work/AuthKey.p8"
printf '%s' "$NOTARY_P8_BASE64" | base64 --decode >"$p8" 2>/dev/null || die "NOTARY_P8_BASE64 não é base64 válido"
chmod 600 "$p8"
# Binário solto não recebe grampo (staple); o Gatekeeper consulta a notarização online.
for a in "${BINS[@]}"; do
  ditto -c -k --keepParent "$DIR/$a" "$work/$a.zip"
  res="$(xcrun notarytool submit "$work/$a.zip" --key "$p8" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" --wait 2>&1)" || { printf '%s\n' "$res" >&2; die "notarytool falhou para $a"; }
  printf '%s\n' "$res"
  # o notarytool pode sair com 0 mesmo com a submissão rejeitada: exige o status Accepted.
  printf '%s\n' "$res" | grep -q 'status: Accepted' || die "notarização de $a não foi aceita"
done

step "checksums.txt (depois da assinatura)"
scripts/release-checksums.sh "$DIR"
cat "$DIR/checksums.txt"
