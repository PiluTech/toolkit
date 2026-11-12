#!/usr/bin/env bash
# Release (plano B local; o caminho normal é .github/workflows/release.yml, docs/release.md, que reusa este
# script sem --sign-macos e assina com scripts/release-sign-macos.sh). Compila os 6 alvos com CGO_ENABLED=0,
# opcionalmente assina e notariza os de macOS, gera o checksums.txt e deixa tudo em dist/.
# NÃO publica nada: a criação da release no GitHub (gh release create) é um passo manual e separado.
#
#   scripts/release.sh --version v0.1.0 [--out dist] [--dry-run] [--sign-macos]
#
# Exige árvore limpa e a tag da versão apontando para HEAD (git tag vX.Y.Z). --out tem de ser um
# subdiretório do repositório (é apagado e recriado).
#
# --dry-run     só imprime os passos (nenhum build, codesign, op ou notarytool roda).
# --sign-macos  liga a assinatura (Developer ID) e a notarização, DESLIGADAS por padrão. Lê de variáveis
#               de ambiente, ou de um arquivo local fora do git (padrão ~/.config/toolkit/release.env,
#               linhas CHAVE=valor, permissão 600, ou TOOLKIT_RELEASE_ENV=<arquivo>):
#                 NOTARY_KEY_ID        id da chave de API do notarytool
#                 NOTARY_ISSUER        issuer da chave de API
#                 NOTARY_P8_OP_REF     referência do 1Password (op read) para o arquivo .p8
#                 TOOLKIT_SIGN_IDENTITY identidade do codesign ("Developer ID Application: ...")
#               Nada disso é gravado no repositório. O .p8 vai para um arquivo temporário 600 apagado
#               por trap e nunca é impresso.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGETS=(windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)

VERSION="" OUT="dist" DRY=0 SIGN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:?--version precisa de valor}"; shift 2 ;;
    --out) OUT="${2:?--out precisa de valor}"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    --sign-macos) SIGN=1; shift ;;
    -h | --help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "release: argumento desconhecido: $1" >&2; exit 2 ;;
  esac
done

die() { echo "release: $*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }
# run executa o comando, ou só o mostra no --dry-run.
run() { if [ "$DRY" = 1 ]; then printf '+ %s\n' "$*"; else "$@"; fi; }

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "use --version vX.Y.Z (recebido: '${VERSION}')"

# O release compila com a toolchain que o go.mod exige (ou mais nova): correções de segurança da biblioteca
# padrão (ex.: net/http) entram no binário. GOTOOLCHAIN=local com um go mais velho é recusado.
check_toolchain() {
  local want have
  want="$(awk '$1 == "go" { print $2; exit }' go.mod)"
  have="$(go env GOVERSION 2>/dev/null)" || die "o go não conseguiu usar a toolchain que o go.mod exige (go $want): $(go env GOVERSION 2>&1 | head -n1)"
  have="${have#go}"
  if [ -z "$want" ] || [ -z "$have" ]; then die "não consegui ler a versão do go (go.mod: '$want', go: '$have')"; fi
  if [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)" != "$want" ]; then
    die "a toolchain ($have) é mais velha que a do go.mod ($want); use GOTOOLCHAIN=auto"
  fi
  echo "toolchain: go$have (go.mod exige go$want)"
}

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

# O arquivo guarda referências de segredo: só o dono pode ler (600 ou 400).
check_env_file_mode() {
  local mode
  mode="$(stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1")"
  case "$mode" in 600 | 400) ;; *) die "$1 deve ter permissão 600 (tem $mode): chmod 600 '$1'" ;; esac
}

# env_get NOME: variável de ambiente, ou a linha NOME=valor do arquivo local fora do git.
env_get() {
  local name="$1" v="${!1:-}" file="${TOOLKIT_RELEASE_ENV:-$HOME/.config/toolkit/release.env}" line
  if [ -z "$v" ] && [ -f "$file" ]; then
    check_env_file_mode "$file"
    line="$(grep -E "^${name}=" "$file" | tail -n1 || true)"
    v="${line#*=}"
    v="${v%\"}"; v="${v#\"}"
  fi
  printf '%s' "$v"
}

sign_macos_check() { # valida o necessário ANTES de compilar qualquer coisa
  local n
  for n in NOTARY_KEY_ID NOTARY_ISSUER NOTARY_P8_OP_REF TOOLKIT_SIGN_IDENTITY; do
    [ -n "$(env_get "$n")" ] || die "--sign-macos: defina $n no ambiente ou no arquivo de release local (fora do git)"
  done
  if [ "$DRY" = 0 ]; then
    local c
    for c in codesign xcrun op; do command -v "$c" >/dev/null 2>&1 || die "--sign-macos: '$c' não encontrado"; done
  fi
}

sign_macos_binaries() {
  local identity a
  identity="$(env_get TOOLKIT_SIGN_IDENTITY)"
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
  scripts/release-checksums.sh "$OUT"
  cat "$OUT/checksums.txt"
}

# O binário leva Commit=HEAD: só compila árvore limpa e com a tag da versão exatamente em HEAD.
check_git_state() {
  [ -z "$(git status --porcelain --untracked-files=all)" ] || die "árvore suja: faça commit ou descarte as mudanças antes da release"
  git rev-parse -q --verify "refs/tags/${VERSION}^{commit}" >/dev/null || die "a tag $VERSION não existe: crie-a em HEAD antes (git tag $VERSION)"
  [ "$(git rev-parse "refs/tags/${VERSION}^{commit}")" = "$(git rev-parse HEAD)" ] || die "a tag $VERSION não aponta para HEAD"
}

# resolve_out: caminho REAL de --out, que tem de ser um subdiretório do repositório (nunca o próprio repo,
# a raiz do disco, .git, um link simbólico ou um diretório com arquivo versionado), pois será apagado.
resolve_out() {
  local root parent base resolved
  [ -n "$OUT" ] || die "--out vazio"
  root="$(pwd -P)"
  base="$(basename "$OUT")"
  case "$base" in . | .. | /) die "--out inválido: $OUT" ;; esac
  [ ! -L "$OUT" ] || die "--out não pode ser link simbólico: $OUT"
  parent="$(cd "$(dirname "$OUT")" 2>/dev/null && pwd -P)" || die "o diretório pai de --out não existe: $OUT"
  resolved="$parent/$base"
  case "$resolved" in
    "$root"/*) ;;
    *) die "--out fora do repositório: $resolved" ;;
  esac
  case "$resolved" in "$root/.git" | "$root/.git/"*) die "--out dentro de .git" ;; esac
  if [ -n "$(git ls-files -- "$resolved")" ]; then die "--out contém arquivos versionados: $resolved"; fi
  OUT="$resolved"
}

step "release $VERSION (dry-run=$DRY, sign-macos=$SIGN)"
check_git_state
check_toolchain
resolve_out
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
