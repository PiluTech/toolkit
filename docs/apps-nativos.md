# `toolkit apps-nativos` (só Windows)

Remove apps nativos opcionais do Windows (pacotes AppX/MSIX como Clima, Notícias, Paciência) de uma lista FECHADA que não quebra o sistema. Mostra o plano antes, pede confirmação, guarda backup e documenta como reinstalar. Nunca eleva sozinho.

## Comandos

| Comando | O que faz |
|---|---|
| `toolkit apps-nativos` | Lista o catálogo com o estado de cada app (instalado ou ausente) e o perfil dele. Só leitura, sem administrador. |
| `toolkit apps-nativos remover (--perfil basico\|completo \| --app id ...) [--todos-usuarios] [--dry-run] [--yes]` | Calcula o plano, imprime, pede `[s/N]` e remove. |
| `toolkit apps-nativos reinstalar` | Lê o backup e IMPRIME como reinstalar cada app removido. Não executa nada. |

## O que remove e o que nunca remove

O catálogo é uma lista fechada de nomes EXATOS de pacote (o campo `Name` do `Get-AppxPackage`), cada um com título em pt-BR, inglês e espanhol. Só entram apps claramente opcionais. Fora do catálogo, nada é removido, e o nome passa por uma regex fechada (`^[A-Za-z0-9][A-Za-z0-9.]{0,100}$`) antes de ir a qualquer script.

Uma lista de negação no código, validada por teste contra o catálogo, protege: Microsoft Store e compra, App Installer (winget), WebView2 e Edge, shell, Iniciar, Configurações, Pesquisa/Cortana, Segurança do Windows (Defender), .NET, VCLibs, UI.Xaml, Windows Terminal, Fotos, Calculadora, Notepad, identidade e serviços de jogos do Xbox e codecs (HEIF, HEVC, VP9, WebP). Na hora de remover, cada item da listagem real é revalidado contra o catálogo E a lista de negação, e nunca se remove pacote `NonRemovable`, de framework ou com assinatura de sistema, mesmo que o nome case.

Dois casos ficam FORA do catálogo de propósito (revisão da Lupa):

- **Obter Ajuda** (`Microsoft.GetHelp`) hospeda os solucionadores de problemas do Windows (Update, rede, áudio, BITS): está na lista de negação e nunca é removido.
- **Mapas** (`Microsoft.WindowsMaps`) e **Visualizador 3D** (`Microsoft.Microsoft3DViewer`) foram retirados da Microsoft Store (julho de 2025 e 1º de julho de 2026): com `--todos-usuarios` o provisionamento sumiria e a reinstalação prometida não existiria. Regra: o toolkit só oferece remover um app quando a reinstalação é real (a Store ainda o tem ou o provisionamento é mantido).

## Perfis

- **basico** (conservador): Bing (Clima e Notícias), Dicas, Paciência, Office Hub, Hub de Comentários, Skype e Realidade Mista.
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

## Na TUI (aba "Apps nativos")

A 6ª aba (`6`, curta "Nativos") usa o mesmo núcleo do comando (mesmo catálogo, plano, backup e regras). Lista o catálogo com o estado (instalado ou ausente) e o perfil de cada app (basico ou completo). Teclas: `espaço` marca/desmarca o app sob o cursor, `b` marca o perfil básico, `c` o completo, `u` liga/desliga "todos os usuários" (exige administrador: sem ele a aba avisa e não liga; o toolkit nunca eleva sozinho), `p` mostra o plano (só leitura), `r` mostra o plano e pergunta `[s/N]` na própria aba (só `s` ou `y` confirmam; qualquer outra tecla cancela), `i` mostra "como reinstalar" lido do backup (só texto, nada é executado). O resultado (quantos foram removidos, onde está o backup, como reinstalar) fica na tela e na linha de status.

Garantias do backend da TUI: o plano é de USO ÚNICO com ID opaco (a confirmação aplica só o plano mostrado, descarta os demais e recusa com `ErrNatStalePlan` se o sistema ou a seleção mudou); `Esc` durante a preparação do plano cancela e uma resposta tardia nunca abre a confirmação; leitura e plano rodam em `tea.Cmd` assíncrono; todo texto externo passa por `textsafe`. Fora do Windows a aba diz que os apps nativos só existem no Windows (no macOS os apps do sistema ficam em volume protegido; no Linux esta versão não remove pacotes da distribuição) e nenhuma tecla muda nada.

## Limites

- Sem desfazer automático (veja acima).
- Só remove o que está no catálogo; não há remoção livre por nome.
- Fora do Windows o comando termina com erro e não toca em nada: no macOS os apps do sistema ficam em volume protegido e não são removidos; no Linux esta versão não remove pacotes da distribuição.
- Texto vindo do sistema passa por `textsafe` antes de ir ao terminal; a interface sai em pt-BR, inglês e espanhol (`TOOLKIT_LANG`).


## Plano de uso único (TUI)

O plano que a TUI mostra guarda uma cópia ESTRUTURADA dos itens (nome, `PackageFullName`, família, publicador, versão, tipo, escopo e marcas). Na confirmação o backend recalcula e só aplica se tudo for idêntico (não só o texto): mudar até o `PackageFullName` (outra arquitetura) entre o plano e o `s` recusa com "o plano mudou", sem remover nada nem gravar backup.
