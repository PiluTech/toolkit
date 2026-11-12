#!/usr/bin/env bash
# Release LOCAL (o GitHub Actions da conta está sem cobrança). Compila os 6 alvos com CGO_ENABLED=0,
# opcionalmente assina e notariza os de macOS, gera o checksums.txt e deixa tudo em dist/.
# NÃO publica nada: a criação da release no GitHub (gh release create) é um passo manual e separado.
#
#   scripts/release.sh --version v0.1.0 [--out dist] [--dry-run] [--sign-macos]
#
# --dry-run     só imprime os passos (nenhum build, codesign, op ou notarytool roda).
# --sign-macos  liga a assinatura (Developer ID) e a notarização, DESLIGADAS por padrão. Lê de variáveis
#               de ambiente, ou de um arquivo local fora do git (padrão ~/.config/toolkit/release.env,
#               linhas CHAVE=valor, permissão 600, ou TOOLKIT_RELEASE_ENV=<arquivo>):
#                 NOTARY_KEY_ID        id da chave de API do notarytool
#                 NOTARY_ISSUER        issuer da chave de API
#                 NOTARY_P8_OP_REF     referência do 1Password (op read) para o arquivo .p8
#                 TOOLKIT_SIGN_IDENTITY (opcional) identidade do codesign
#               Nada disso é gravado no repositório. O .p8 vai para um arquivo temporário 600 apagado
#               por trap e nunca é impresso.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGETS=(windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)
DEFAULT_IDENTITY='Developer ID Application: Paulo Victor Torres Silva (WU7Z54BRYQ)'

VERSION="" OUT="dist" DRY=0 SIGN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:?--version precisa de valor}"; shift 2 ;;
    --out) OUT="${2:?--out precisa de valor}"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    --sign-macos) SIGN=1; shift ;;
    -h | --help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "release: argumento desconhecido: $1" >&2; exit 2 ;;
  esac
done

die() { echo "release: $*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }
# run executa o comando, ou só o mostra no --dry-run.
run() { if [ "$DRY" = 1 ]; then printf '+ %s\n' "$*"; else "$@"; fi; }

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "use --version vX.Y.Z (recebido: '${VERSION}')"

asset_name() { # alvo os/arch -> nome do arquivo
  local os="${1%/*}" arch="${1#*/}" ext=""
  [ "$os" = windows ] && ext=".exe"
  echo "toolkit_${os}_${arch}${ext}"
}

build_all() {
  local module commit date target
  module="$(go list -m)"
  commit="$(git rev-parse --short HEAD)"
  date="$(git log -1 --format=%cd --date=format:%Y-%m-%dT%H:%M:%SZ)"
  local ldflags="-s -w -X ${module}/internal/buildinfo.Version=${VERSION} -X ${module}/internal/buildinfo.Commit=${commit} -X ${module}/internal/buildinfo.Date=${date}"
  for target in "${TARGETS[@]}"; do
    echo "build $target"
    if [ "$DRY" = 1 ]; then
      printf '+ CGO_ENABLED=0 GOOS=%s GOARCH=%s go build -trimpath -buildvcs=false -ldflags "%s" -o %s/%s ./cmd/toolkit\n' \
        "${target%/*}" "${target#*/}" "$ldflags" "$OUT" "$(asset_name "$target")"
    else
      CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" \
        go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$OUT/$(asset_name "$target")" ./cmd/toolkit
    fi
  done
}

# ---- macOS: assinatura e notarização (funções separadas, só com --sign-macos) ----------------------

# env_get NOME: variável de ambiente, ou a linha NOME=valor do arquivo local fora do git.
env_get() {
  local name="$1" v="${!1:-}" file="${TOOLKIT_RELEASE_ENV:-$HOME/.config/toolkit/release.env}" line
  if [ -z "$v" ] && [ -f "$file" ]; then
    line="$(grep -E "^${name}=" "$file" | tail -n1 || true)"
    v="${line#*=}"
    v="${v%\"}"; v="${v#\"}"
  fi
  printf '%s' "$v"
}

sign_macos_check() { # valida o necessário ANTES de compilar qualquer coisa
  local n
  for n in NOTARY_KEY_ID NOTARY_ISSUER NOTARY_P8_OP_REF; do
    [ -n "$(env_get "$n")" ] || die "--sign-macos: defina $n no ambiente ou no arquivo de release local (fora do git)"
  done
  if [ "$DRY" = 0 ]; then
    local c
    for c in codesign xcrun op; do command -v "$c" >/dev/null 2>&1 || die "--sign-macos: '$c' não encontrado"; done
  fi
}

sign_macos_binaries() {
  local identity="${TOOLKIT_SIGN_IDENTITY:-$DEFAULT_IDENTITY}" a
  for a in toolkit_darwin_amd64 toolkit_darwin_arm64; do
    run codesign --force --options runtime --timestamp --sign "$identity" "$OUT/$a"
    run codesign --verify --strict --verbose=2 "$OUT/$a"
  done
}

notarize_macos_binaries() {
  local keyid issuer opref p8 zip a
  keyid="$(env_get NOTARY_KEY_ID)"; issuer="$(env_get NOTARY_ISSUER)"; opref="$(env_get NOTARY_P8_OP_REF)"
  if [ "$DRY" = 1 ]; then
    echo "+ (umask 077; op read \$NOTARY_P8_OP_REF > <arquivo temporário 600, apagado por trap>)"
    for a in toolkit_darwin_amd64 toolkit_darwin_arm64; do
      echo "+ ditto -c -k --keepParent $OUT/$a <tmp>/$a.zip"
      echo "+ xcrun notarytool submit <tmp>/$a.zip --key <p8 temporário> --key-id \$NOTARY_KEY_ID --issuer \$NOTARY_ISSUER --wait"
    done
    return
  fi
  # Binário solto não recebe grampo (staple); o Gatekeeper consulta a notarização online.
  p8="$(umask 077; mktemp "${TMPDIR:-/tmp}/toolkit-p8.XXXXXX")"
  zip="$(mktemp -d "${TMPDIR:-/tmp}/toolkit-notary.XXXXXX")"
  trap 'rm -rf "$p8" "$zip"' EXIT INT TERM
  chmod 600 "$p8"
  op read "$opref" >"$p8"
  for a in toolkit_darwin_amd64 toolkit_darwin_arm64; do
    ditto -c -k --keepParent "$OUT/$a" "$zip/$a.zip"
    xcrun notarytool submit "$zip/$a.zip" --key "$p8" --key-id "$keyid" --issuer "$issuer" --wait
  done
  rm -rf "$p8" "$zip"
  trap - EXIT INT TERM
}

# ---- empacotamento ----------------------------------------------------------------------------------

write_checksums() { # SEMPRE depois da assinatura: assinar muda o binário
  if [ "$DRY" = 1 ]; then echo "+ (cd $OUT && sha256 de todos os arquivos > checksums.txt)"; return; fi
  ( cd "$OUT"
    export LC_ALL=C
    sums=""
    for f in *; do
      if command -v sha256sum >/dev/null 2>&1; then sums+="$(sha256sum "$f")"$'\n'; else sums+="$(shasum -a 256 "$f")"$'\n'; fi
    done
    printf '%s' "$sums" >checksums.txt )
  cat "$OUT/checksums.txt"
}

step "release $VERSION (dry-run=$DRY, sign-macos=$SIGN)"
[ "$SIGN" = 1 ] && sign_macos_check
if [ "$DRY" = 0 ]; then rm -rf "${OUT:?}"; mkdir -p "$OUT"; else echo "+ rm -rf $OUT && mkdir -p $OUT"; fi

step "compilação dos 6 alvos (CGO_ENABLED=0)"
build_all

step "bootstraps"
run cp scripts/bootstrap/unix.sh "$OUT/unix.sh"
run cp scripts/bootstrap/win.ps1 "$OUT/win.ps1"

if [ "$SIGN" = 1 ]; then
  step "assinatura macOS (codesign)"; sign_macos_binaries
  step "notarização macOS (notarytool)"; notarize_macos_binaries
else
  echo "(assinatura e notarização do macOS desligadas: use --sign-macos)"
fi

step "checksums.txt"
write_checksums

step "pronto"
echo "Arquivos em $OUT/. Para publicar (passo manual, com OK do dono): gh release create $VERSION $OUT/* --draft"
