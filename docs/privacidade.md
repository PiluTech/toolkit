# `toolkit privacidade` (só Windows)

Reduz a coleta de dados do Windows em dois perfis. Mostra o plano antes, guarda o valor original de cada item e desfaz tudo. Nunca eleva sozinho e nunca edita o arquivo `hosts` nem remove componente do sistema.

## Comandos

| Comando | O que faz |
|---|---|
| `toolkit privacidade` | Só mostra o estado atual e o plano de cada perfil. Não muda nada e não exige administrador. |
| `toolkit privacidade aplicar --perfil basico\|recomendado [--dry-run] [--yes] [--somente-usuario]` | Calcula o plano, imprime o que muda (valor atual -> novo, e o que exige administrador, com o comando manual que desfaz cada item), pede `[s/N]` e aplica. |
| `toolkit privacidade desfazer [--dry-run] [--yes]` | Mostra o que será restaurado a partir do backup e restaura. |

Fora do Windows os três comandos terminam com a mensagem "só existe no Windows" e código de saída 1, sem tocar em nada.

## Perfis

- **basico**: só o que é do usuário (chaves em `HKCU`). Não exige administrador. ID de publicidade, experiências personalizadas com dados de diagnóstico, sugestões e anúncios do Iniciar, instalação automática de apps sugeridos, dicas e boas-vindas, pedidos de feedback.
- **recomendado**: o básico mais política de dados de diagnóstico (`HKLM`), histórico de atividades, o serviço `DiagTrack` (desativado e parado; o `dmwappushservice` fica de fora porque desativá-lo quebra a sincronização com Intune/MDM) e as tarefas agendadas de coleta (Appraiser, ProgramDataUpdater, CEIP, coletor de diagnóstico de disco). Exige administrador na hora de aplicar.

Limite real: no Windows Home e Pro o mínimo de dados de diagnóstico é "obrigatório" (valor 1); o Windows ignora 0 nessas edições. O plano avisa isso. O texto de cada item (o que faz e por quê) sai do próprio comando, em pt-BR, inglês ou espanhol (`TOOLKIT_LANG`).

## Fluxo do `aplicar`

1. Lê o estado atual (só leitura) e monta o plano. O plano é sempre impresso.
2. `--dry-run` para aqui. Nada é alterado.
3. Se o plano tem itens de administrador e o terminal não é administrador, o comando recusa ANTES de mudar qualquer coisa e manda abrir o terminal como administrador e rodar de novo. O toolkit nunca pede elevação sozinho.
4. Confirmação (CLI): com terminal pergunta `[s/N]` (só s, sim, y, yes confirmam); sem terminal exige `--yes` (erro antes de qualquer mudança).
4b. Depois da confirmação (e também com `--yes`, no `aplicar` e no `desfazer`) o CLI RECALCULA o plano (no `desfazer`, relê também o backup) e só segue se for idêntico ao mostrado; senão recusa com "o estado mudou desde o plano mostrado; nada foi alterado", sem backup nem escrita.
5. Grava o backup e só então muda, item a item. Se algo falha, para e informa quantos itens já foram aplicados; o backup já tem os originais.

`--somente-usuario` aplica só os itens do usuário (`HKCU`) e ignora os de administrador; serve para quem não pode abrir o terminal elevado. Com o perfil `recomendado` isso equivale ao `basico`.

## Backup e desfazer

- Arquivos na pasta de estado do toolkit (`C:\ProgramData\toolkit` como administrador, `%LOCALAPPDATA%\toolkit` como usuário), permissão 0600, escrita atômica, e SEPARADOS por escopo: `privacidade-backup-<SID>.json` (só os valores `HKCU`, de cada usuário; o SID vem de `whoami /user`) e `privacidade-backup-maquina.json` (`HKLM`, serviços e tarefas, compartilhados). Cada arquivo grava o dono; um arquivo de outro dono (copiado ou renomeado) é recusado e fica intacto. Dois administradores não compartilham nem desfazem o `HKCU` um do outro.
- O backup é tratado como dado não confiável: ANTES de ler ou mudar qualquer coisa, o `desfazer` confere CADA entrada contra o catálogo (ID, chave e nome, serviço ou caminho da tarefa idênticos aos do catálogo; tipo de valor e de início conhecidos). Entrada fora do catálogo ou de tipo desconhecido recusa o desfazer inteiro, sem mutação, e o arquivo é preservado. O `desfazer --dry-run` nunca remove nem escreve nada, nem com zero pendências.
- Guarda o estado ORIGINAL de cada item (valor do registro ou "não existia", tipo de início e se rodava o serviço, se a tarefa estava habilitada). Aplicar duas vezes nunca sobrescreve o original.
- `desfazer` restaura o valor (ou apaga o que não existia), reconfigura serviços e tarefas e, no fim, apaga o backup. Falha no meio deixa no backup só o que sobrou.
- Como o backup fica na pasta de estado de quem aplicou, rode o `desfazer` no mesmo nível (administrador ou usuário) em que rodou o `aplicar`.
- Desfazer o que mexe em `HKLM`, serviços ou tarefas também exige administrador (mesma recusa antes de mudar).
- Sem backup: "Não há backup de privacidade: nada a desfazer" (erro).

## Como testar com segurança

```text
toolkit privacidade                                   # estado e planos, nada muda
toolkit privacidade aplicar --perfil recomendado --dry-run
```

Os testes automáticos (`internal/cli/privacidade_test.go`, `internal/privacidade`) usam um Windows em memória: nenhum teste toca o registro, os serviços ou as tarefas reais. Todo texto vindo do sistema passa por `textsafe` antes de chegar ao terminal.

## Formato do backup

O formato do backup nasce na v0.4.0-rc.1 (nunca houve outro publicado): não há migração. O `desfazer` valida cada entrada contra o catálogo atual, sem exceções; backup que cite algo fora do catálogo é recusado sem mudar nada.

## Na TUI (aba Privacidade)

**Confirmação na TUI** (`internal/tui/confirm.go`, componente único das duas abas): ao pedir aplicar/restaurar/remover, a aba mostra, nesta ordem, o título, o resumo, o aviso (administrador, sem desfazer automático), o BACKUP, o COMO DESFAZER, o plano inteiro (rolável com `↑↓`/`PgUp`/`PgDn`, com `↓ mais N linhas` quando longo) e só depois os botões. O foco inicial é `>[ Cancelar ]<` (padrão seguro); `Tab`/`Shift+Tab`/`←→`/`h l` movem o foco, `Enter` ativa o botão focado, `s` ou `y` confirmam direto, `n` e `Esc` cancelam e qualquer outra tecla não faz nada (nunca confirma). Plano com itens de administrador num terminal sem administrador também abre a confirmação, mas o botão vira `( Aplicar: sem administrador )` e não ativa: só Cancelar funciona, com a instrução de abrir o terminal como administrador. A linha de status mostra só a dica de teclas. Sem cor (`NO_COLOR`) tudo sai por símbolo e palavra.

Os botões são `Aplicar` (perfil) e `Restaurar` (desfazer; `Remover backup` quando já está tudo como antes). O plano exibido é o mesmo que o backend devolveu (uso único, por ID); a confirmação só aplica esse plano.

**Faixa de resultado** (`internal/tui/result.go`, PR2 do redesign): depois de aplicar ou restaurar, o resultado aparece NA PRÓPRIA aba, logo abaixo do cabeçalho, e fica até a pessoa teclar `x` (não some por tempo nem ao trocar de aba). Traz símbolo e palavra (`✓ feito`, `! parcial`, `✗ falhou`, `i info`), o que foi feito, quantas mudanças, onde está o backup e COMO DESFAZER (`Para desfazer: tecle d nesta aba`). Falha no meio vira `! parcial` (quantas mudanças ficaram, o erro, o backup e o `d`); recusa (precisa de administrador, plano mudou) diz que NADA foi alterado. A linha de estado fica só com um resumo curto. A aba implementa `Resulter` (`Result()`/`Dismiss()`).
