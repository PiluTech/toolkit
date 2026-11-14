# `toolkit apps-nativos` (só Windows)

Remove apps nativos opcionais do Windows (pacotes AppX/MSIX como Clima, Notícias, Paciência, Obter Ajuda) de uma lista FECHADA que não quebra o sistema. Mostra o plano antes, pede confirmação, guarda backup e documenta como reinstalar. Nunca eleva sozinho.

## Comandos

| Comando | O que faz |
|---|---|
| `toolkit apps-nativos` | Lista o catálogo com o estado de cada app (instalado ou ausente) e o perfil dele. Só leitura, sem administrador. |
| `toolkit apps-nativos remover (--perfil basico\|completo \| --app id ...) [--todos-usuarios] [--dry-run] [--yes]` | Calcula o plano, imprime, pede `[s/N]` e remove. |
| `toolkit apps-nativos reinstalar` | Lê o backup e IMPRIME como reinstalar cada app removido. Não executa nada. |

## O que remove e o que nunca remove

O catálogo é uma lista fechada de nomes EXATOS de pacote (o campo `Name` do `Get-AppxPackage`), cada um com título em pt-BR, inglês e espanhol. Só entram apps claramente opcionais. Fora do catálogo, nada é removido, e o nome passa por uma regex fechada (`^[A-Za-z0-9][A-Za-z0-9.]{0,100}$`) antes de ir a qualquer script.

Uma lista de negação no código, validada por teste contra o catálogo, protege: Microsoft Store e compra, App Installer (winget), WebView2 e Edge, shell, Iniciar, Configurações, Pesquisa/Cortana, Segurança do Windows (Defender), .NET, VCLibs, UI.Xaml, Windows Terminal, Fotos, Calculadora, Notepad, identidade e serviços de jogos do Xbox e codecs (HEIF, HEVC, VP9, WebP). Na hora de remover, cada item da listagem real é revalidado contra o catálogo E a lista de negação, e nunca se remove pacote `NonRemovable`, de framework ou com assinatura de sistema, mesmo que o nome case.

## Perfis

- **basico** (conservador): Bing (Clima e Notícias), Obter Ajuda, Dicas, Paciência, Office Hub, Hub de Comentários, Skype, Realidade Mista e Visualizador 3D.
- **completo**: o básico mais o restante do catálogo.
- `--app id` (pode repetir) escolhe apps avulsos pelo id do catálogo; vale sozinho ou junto de `--perfil`.

Sem perfil e sem `--app` o comando recusa.

## Escopo e administrador

- Padrão: só o usuário atual, sem administrador (`Remove-AppxPackage`).
- `--todos-usuarios`: também os outros usuários e o provisionamento (`Remove-AppxPackage -AllUsers` e `Remove-AppxProvisionedPackage -Online`), o que impede o app de voltar em contas novas. Exige administrador. Sem administrador o comando recusa ANTES de mudar qualquer coisa e manda abrir o terminal como administrador. O toolkit nunca pede elevação sozinho.

## Fluxo do `remover`

1. Lê os pacotes instalados (só leitura) e monta o plano: cada app instalado que será removido, com título, nome do pacote, versão e escopo. Os já ausentes ficam fora. O plano é SEMPRE impresso.
2. `--dry-run` para aqui. Nada é alterado.
3. `--todos-usuarios` sem administrador: recusa.
4. Confirmação: com terminal pergunta `[s/N]`; sem terminal exige `--yes` (erro antes de qualquer mudança).
5. Grava o backup e só então remove, app a app. Se algo falha, para e informa quantos já foram removidos.

## Backup

Antes de remover, o toolkit grava em `apps-nativos-<SID>.json` (permissão 0600, escrita atômica, na pasta de estado do toolkit, um arquivo por usuário) a lista dos apps removidos: nome, nome completo do pacote, família, versão, editor, escopo e data. O arquivo é validado contra o catálogo e contra o dono (SID) antes de qualquer uso.

## Como reinstalar

NÃO existe desfazer automático confiável: o pacote some do disco. `toolkit apps-nativos reinstalar` lê o backup e imprime, sem executar:

- `winget install --id <id da Store> --source msstore`, só para apps cujo id da Store está no catálogo;
- senão, "abra a Microsoft Store e procure por <título>";
- se o provisionamento tinha sido mantido: `Add-AppxPackage -RegisterByFamilyName -MainPackage <família>`.

Reinstalar pode exigir a Microsoft Store e uma conta.

## Limites

- Sem desfazer automático (veja acima).
- Só remove o que está no catálogo; não há remoção livre por nome.
- Fora do Windows o comando termina com erro e não toca em nada: no macOS os apps do sistema ficam em volume protegido e não são removidos; no Linux esta versão não remove pacotes da distribuição.
- Texto vindo do sistema passa por `textsafe` antes de ir ao terminal; a interface sai em pt-BR, inglês e espanhol (`TOOLKIT_LANG`).
