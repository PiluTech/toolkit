# Release do toolkit

A release sai pela pipeline `.github/workflows/release.yml`, sem depender do Mac de ninguém. Ela dispara **só**
por push de tag `vX.Y.Z` (nunca por pull request, push comum nem manualmente: o repositório é público e PR de
fork não pode ver segredo) e termina em um **rascunho** de release no GitHub. Quem publica é o Paulo, depois de
conferir.

## Passo a passo

1. Com a `main` no estado a lançar (gate verde, `docs/` e `CLAUDE.md` em dia), crie a tag **no commit da main**
   e envie só ela (a pipeline recusa tag cujo commit não esteja na main):
   ```
   git switch main && git pull --ff-only
   git tag -a vX.Y.Z -m "toolkit vX.Y.Z"
   git push origin vX.Y.Z
   ```
2. O job **build** (ubuntu) roda: confere a tag, instala o golangci-lint verificado por SHA-256
   (`scripts/instalar-ferramentas.sh`), roda `scripts/ci.sh` (o mesmo gate local), depois
   `scripts/release.sh --version vX.Y.Z --out dist` (6 binários com `CGO_ENABLED=0`, `unix.sh`, `win.ps1` e
   `checksums.txt`; o script segue exigindo árvore limpa, tag = HEAD e `--out` dentro do repo). Sobe o artefato
   `dist-build`.
3. O job **sign-macos** (macos-latest) fica **esperando a aprovação** do environment `release`. No GitHub:
   Actions, a execução, "Review deployments", aprovar. Aprovar é a decisão "esta tag pode usar o certificado".
   Depois da aprovação ele roda `scripts/release-sign-macos.sh`: cria uma keychain temporária, importa o
   certificado Developer ID, assina os 2 binários macOS (`codesign --options runtime --timestamp`), notariza com
   o `notarytool` (exige `status: Accepted`), **recalcula o `checksums.txt` depois da assinatura**, apaga a
   keychain e os temporários (por `trap`, mesmo em falha) e confere que o binário darwin assinado roda e imprime a
   versão da tag. Sobe o artefato `dist-signed`.
4. O job **draft** (ubuntu, o único com `contents: write`) baixa `dist-signed`, confere `sha256sum -c
   checksums.txt`, que os 8 arquivos estão listados e que `toolkit_linux_amd64 version` traz a tag, e cria o
   release como **rascunho** (`gh release create --draft --verify-tag`) com as notas do modelo
   `.github/release-notes.md.tmpl` (`{{VERSION}}` vira a tag).
5. O Paulo abre o rascunho em Releases, troca os trechos `(preencher)` das notas (o que mudou, limites
   conhecidos), confere os 9 arquivos e **publica**. Rascunho que não vai sair: apague o rascunho e a tag
   (`git push origin :refs/tags/vX.Y.Z`).
6. Falhou no meio? Corrija e crie uma tag nova (patch seguinte). Para repetir a mesma tag, apague antes o
   rascunho (o `gh release create` recusa duplicata) e a tag, e recrie a tag no commit certo.

Depois de publicar, confira de uma máquina limpa: `unix.sh`/`win.ps1` da release, `toolkit version`, e
`codesign --verify --strict` + `spctl -a -vv -t exec` no binário macOS baixado.

## Segredos e environment `release`

Nada de segredo no código, no workflow ou no log. Os cinco segredos vivem no **environment `release`** (Settings,
Environments), nunca como segredo de repositório, e só o job `sign-macos` (que declara `environment: release`) os
lê, por `env:` de um único passo. O script não imprime nenhum valor, mascara com `::add-mask::`, e guarda
`.p12`, `.p8` e zips em `mktemp -d` modo 700, apagados por `trap`.

| Segredo | O que é |
|---|---|
| `DEVELOPER_ID_P12_BASE64` | certificado **Developer ID Application** (com a chave privada) exportado como `.p12`, em base64 |
| `DEVELOPER_ID_P12_PASSWORD` | senha do `.p12` |
| `NOTARY_KEY_ID` | id da chave da API do App Store Connect ("PiluTech Maestro"); não é segredo de verdade, fica aqui por convenção |
| `NOTARY_ISSUER` | issuer da mesma chave; idem |
| `NOTARY_P8_BASE64` | a chave `.p8`, em base64 |

Estado do environment (conferido por `gh api` em 2026-11-12, somente leitura): já existe `release`, com **revisor
obrigatório** (o Paulo; `prevent_self_review` desligado, então ele aprova a própria execução, o que é o esperado
com um único mantenedor) e política de deploy restrita à **tag `v*`**. O plano da organização é **free**, e
revisor obrigatório em environment é permitido em repositório **público** nesse plano. Se o repositório virar
privado, o plano free perde os revisores obrigatórios e a restrição de tags do environment: nesse caso, ou
migre para um plano pago, ou troque a proteção por uma regra de tag (ruleset de tags `v*` com push restrito ao
dono) e mantenha o `release.yml` só por tag, sem aprovar nada automático.

### Cadastro (Paulo, uma vez, com Touch ID)

O valor sai do 1Password direto para o `gh`, sem passar por variável, arquivo nem log:

```
# .p8 da API do notarytool (referência confirmada)
op read "op://Personal/y5gellouprro6hlt7fvkx7zpgu/Section_3nvmb2qq4edywm3ruld5gkmube/p8" | base64 | tr -d '\n' \
  | gh secret set NOTARY_P8_BASE64 --env release --repo PiluTech/toolkit
```

**A confirmar** (não adivinhe os nomes dos campos): o item é "Apple: certificados de assinatura (Developer ID +
Apple Development)". Os nomes têm `:`, `(`, `+` e espaços, então use o **id** do item na referência:

```
op item get "Apple: certificados de assinatura (Developer ID + Apple Development)" --format json | jq -r '.id'
op item get "Apple: certificados de assinatura (Developer ID + Apple Development)" --format json | jq -r '.fields[].label'
op item get "Apple: certificados de assinatura (Developer ID + Apple Development)" --format json | jq -r '.files[]?.name'
```

(Os dois últimos listam só **rótulos** de campos e nomes de anexos, nunca valores.) Com o id e os rótulos, ajuste
`<ITEM_ID>`, `<CAMPO_DA_SENHA>` e `<ANEXO_P12>` (o `.p12` costuma ser um anexo; se for campo, use o rótulo
dele). O `.p12` tem de ser o do **Developer ID Application** com a chave privada, não o Apple Development:

```
op read "op://Personal/<ITEM_ID>/<ANEXO_P12>" | base64 | tr -d '\n' \
  | gh secret set DEVELOPER_ID_P12_BASE64 --env release --repo PiluTech/toolkit
op read "op://Personal/<ITEM_ID>/<CAMPO_DA_SENHA>" \
  | gh secret set DEVELOPER_ID_P12_PASSWORD --env release --repo PiluTech/toolkit
```

`NOTARY_KEY_ID` e `NOTARY_ISSUER` (App Store Connect, Users and Access, Integrations, a chave "PiluTech Maestro")
não são segredo e **não ficam escritos neste repositório**; sem argumento o `gh` pede o valor no terminal:

```
gh secret set NOTARY_KEY_ID --env release --repo PiluTech/toolkit
gh secret set NOTARY_ISSUER --env release --repo PiluTech/toolkit
```

Conferir só os nomes (sem valores): `gh secret list --env release --repo PiluTech/toolkit`.
Rotação: refaça o comando do segredo; revogue a chave antiga no App Store Connect / Apple Developer.

## O que a pipeline garante (e o que não)

- Gatilho só por tag `v*.*.*`; `permissions: {}` no topo, `contents: read` nos jobs de build e assinatura,
  `contents: write` só no job que cria o rascunho; `concurrency` por tag **sem** cancelar release em andamento;
  `timeout-minutes` em todo job; `persist-credentials: false` em todo checkout; actions fixadas por SHA de commit
  (versão em comentário); sem `pull_request_target`; sem `curl | bash` de terceiros.
- `scripts/check-workflows.sh` (no `scripts/ci.sh`, com autoteste em `scripts/check-workflows_test.sh`, e o
  actionlint quando instalado) reprova qualquer uma dessas regras que alguém afrouxe no YAML.
- Os testes dos scripts de assinatura (`scripts/release-sign-macos_test.sh`) usam ferramentas falsas
  (`security`, `codesign`, `xcrun`, `ditto`): conferem o modo 600 dos temporários, a remoção da keychain e dos
  arquivos mesmo em falha, que nenhum segredo vai para a saída e que o `checksums.txt` bate com os binários
  assinados. A assinatura e a notarização reais só acontecem na primeira release pela pipeline; confira o
  primeiro rascunho com atenção.
- Limite: quem tem permissão de escrita e aprova o próprio deploy pode lançar; a proteção é a política de tag,
  o revisor e o histórico (todo uso do environment aparece em Deployments).

## Custo (minutos de Actions)

- **Repositório público** (o caso do toolkit): os runners hospedados padrão (ubuntu, windows, macOS) são
  **gratuitos e sem limite de minutos** em repositório público; não há multiplicador a pagar. Fonte: documentação
  "About billing for GitHub Actions" (a regra vale para runners padrão; runners maiores têm cobrança própria).
  Confirme na página de billing da organização ao lado da primeira release.
- **Se o repositório virar privado**: o plano free inclui 2.000 minutos por mês e o macOS conta **10x** (Linux 1x).
  Estimativa por release (a calibrar na primeira execução):

  | Job | Tempo | Fator | Minutos cobrados |
  |---|---|---|---|
  | build (ubuntu: ci.sh com race e govulncheck, release.sh) | ~10 min | 1x | ~10 |
  | sign-macos (setup ~1 min, codesign ~1 min, notarização 2 a 10 min por binário, em fila: ~5 a ~20 min; fora a espera de aprovação, que não conta) | ~10 min | 10x | ~100 |
  | draft (ubuntu) | ~1 min | 1x | ~1 |
  | **Total** | | | **~110 por release** (faixa ~60 a ~230) |

  Ou seja, ~18 releases por mês nos 2.000 minutos, ~9 no pior caso de notarização lenta. O `ci.yml` (manual) com a
  matriz ubuntu/windows/macos soma sozinho ~10x no macOS: não o dispare à toa em repo privado. A espera da
  aprovação do environment não consome minuto (o job fica pendente).

## Plano B: release manual no Mac

Se o GitHub Actions estiver fora do ar ou os segredos se perderem, o processo antigo continua valendo:

```
git tag vX.Y.Z            # em HEAD, árvore limpa
scripts/release.sh --version vX.Y.Z --sign-macos     # lê ~/.config/toolkit/release.env (600), fora do git
gh release create vX.Y.Z dist/* --draft --verify-tag --title "vX.Y.Z" --notes-file <notas>
```

Variáveis do `release.env` (referências do 1Password, nada de valor): `NOTARY_KEY_ID`, `NOTARY_ISSUER`,
`NOTARY_P8_OP_REF`, `TOOLKIT_SIGN_IDENTITY`. Detalhes no cabeçalho do `scripts/release.sh`. `--dry-run` só mostra
os passos.
