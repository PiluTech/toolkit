# `toolkit usb`: modo empresa, USB permitidos (Windows, EXPERIMENTAL)

> ## EXPERIMENTAL: ainda não rodou numa máquina real
>
> Esta função foi implementada (PR14 do [plano](plano-seguranca-e-tui.md)) **sem** o smoke numa VM de laboratório que a decisão 12 do plano pedia. O Paulo e o
> Maestro mandaram implementar mesmo assim, **como experimental**. Tudo o que depende do comportamento real do Windows está marcado **A VERIFICAR em VM**
> (lista completa no fim). **Nada se aplica por padrão**: só `toolkit usb aplicar` (depois do plano e do `[s/N]`) ou a ação da TUI (depois da confirmação).
> Não use numa máquina de produção antes de rodar o roteiro de VM abaixo.

> ## LIMITE HONESTO
>
> - **Um dispositivo que CLONA o VID/PID de um permitido PASSA.** Os IDs são anunciados pelo próprio dispositivo; um bad USB que copia o ID de um teclado
>   da lista entra.
> - A política só controla a **instalação ou atualização de driver** de dispositivo novo. **O que já está instalado continua funcionando.**
> - **A política nega só as duas classes de USB (`USB` e `USBDevice`).** Um dispositivo HID **novo e simples** (não composto) costuma ter o nó USB de classe
>   `HIDClass` ("USB Input Device"), que **nenhuma regra nega**: o avaliador do toolkit o considera permitido mesmo fora da lista, e esse é justamente o bad USB
>   mais simples (um teclado falso). Se isso se confirmar na VM, a política **não barra** esse tipo de dispositivo (A VERIFICAR em VM, item 3 do fim; o
>   plano mostra "a política NÃO o bloquearia" para o dispositivo desmarcado de classe não negada, em vez de dizer "BLOQUEADO").
> - **Não é defesa contra atacante com acesso físico que conheça os IDs.** Isso exige controle de endpoint gerenciado (MDM/EDR) e bloqueio físico das portas.
> - Mensagem do toolkit em todos os fluxos: "Isto reduz o risco, não o elimina."

## O que faz

Grava a política oficial "Restrições de instalação de dispositivos" em `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions`, **em ordem de
camadas** (`AllowDenyLayered`), com:

- `AllowDeviceIDs`: os IDs de **hardware** (com `VID_xxxx&PID_xxxx`) de **todos os nós** dos USB conectados e marcados (dispositivo, interfaces `&MI_xx`, nós
  HID), mais um conjunto **fechado** de 6 IDs genéricos de controlador e hub raiz (`PCI\CC_0C03`, `PCI\CC_0C0330`, `PNP0CA1`, `USB\ROOT_HUB20`,
  `USB\ROOT_HUB30`, `USB\USB20_HUB`). **ID compatível genérico (classe HID, `USB\COMPOSITE`...) nunca entra.**
- `DenyDeviceClasses`: **só as duas classes de USB** (`{36fc9e60-c465-11cf-8056-444553540000}` e `{88BAE032-5A81-49f0-BC3D-A4FF138216D6}`).

**Nunca** escreve `DenyUnspecified` (global: valeria para Bluetooth, PS/2, I2C, NVMe, monitor, impressora...), nunca bloqueia por classe `Keyboard`, `Mouse`,
`HIDClass` ou `DiskDrive`, nunca liga a opção retroativa, nada fora da lista fechada de alvos (`checkRel` recusa qualquer outro caminho antes de executar).

## Comandos

```
toolkit usb                                   estado (só leitura, sem administrador)
toolkit usb aplicar [--prazo N] [--dry-run] [--yes]
toolkit usb confirmar
toolkit usb desfazer [--dry-run] [--yes]
```

`aplicar` mostra o plano (dispositivos, a PROVA, ordem, prazo, recuperação) e só depois do `[s/N]` (foco em Cancelar; sem terminal exige `--yes`) executa. O plano
é **recalculado depois da confirmação** e tem de ser idêntico ao mostrado (vale com `--yes`). Fora do Windows: "só existe no Windows", sem tocar em nada
(Linux é o PR15; macOS, o PR16).

`reverter` é um subcomando **oculto** que a tarefa de reversão executa (o mesmo caminho do `desfazer`, sem pergunta).

### Decisões (recomendação do plano, aplicada)

| # | Decisão | Aplicado |
|---|---|---|
| 5 | USB no Windows Home | **Recusa** (a Microsoft não lista a edição). Só Pro, Enterprise e Education passam; Server, IoT e as demais também são recusadas |
| 6 | Identificar por ID de hardware ou de instância | **ID de hardware** (VID/PID, todos os nós). Instância só se a VM provar que é estável (A VERIFICAR) |
| 7 | Prazo da reversão | **10 minutos**, `--prazo N`, **mínimo 2**, máximo 60 |
| 12 | VM antes do PR14 | **Não houve VM**: por isso a função é experimental. O Maestro e o Paulo autorizaram |

Decisões de desenho registradas neste PR (a revisar na VM):

1. **Política pré-existente = recusa.** Se a chave `Restrictions` já tem **qualquer** valor ou subchave (GPO, MDM, outra ferramenta ou uma aplicação anterior), o
   `aplicar` recusa. Ligar `AllowDenyLayered` muda a precedência de políticas que o toolkit não modela, e a prova não saberia avaliá-las. O desfazer, por outro
   lado, **preserva** o que outra origem acrescentar depois (e para se um valor que o toolkit gravou foi alterado por fora).
2. A chave `Restrictions` vazia pré-existente é aceita; o snapshot registra que ela já existia e o desfazer não a apaga.
3. A tarefa de reversão executa uma **cópia do próprio toolkit** na pasta protegida (hash SHA-256 conferido contra o binário em execução), não um script
   embutido: a lógica de restauração fica em Go testado.
4. **Edições:** só Pro, Enterprise e Education passam. IoT Enterprise, Server e as demais são recusadas, mesmo que a fonte do plano liste IoT Enterprise: não
   foi possível confirmar o `EditionID` dela e o toolkit prefere recusar. A revisar na VM.
5. **A pasta protegida é a do estado do administrador.** `C:\ProgramData\toolkit` também guarda o estado das receitas, os logs e os backups da
   privacidade (sempre de processos elevados; sem elevação o toolkit usa `%LOCALAPPDATA%\toolkit`). Trocar a ACL dela (dono Administradores, só SYSTEM e
   Administradores, sem herança) vale para tudo isso. A VERIFICAR em VM que nada não elevado precisa ler ali; se precisar, mover o backup do USB para uma subpasta.
6. **A TUI não lê o PnP ao abrir.** A leitura varre todos os dispositivos e leva vários segundos; a seção mostra "ainda não lido" e a primeira leitura é a
   tecla `c` (como na Otimização). O subcomando oculto `usb reverter` também não consulta versão nova.

## A PROVA de dispositivos essenciais

Antes de gravar qualquer chave o toolkit lê o PnP (`Get-PnpDevice -PresentOnly` e `Get-PnpDeviceProperty` com `DEVPKEY_Device_Parent`/`HardwareIds`/
`CompatibleIds`, PowerShell por caminho absoluto, script ASCII fixo, só leitura), monta a **árvore de pais** e **avalia a política candidata contra ela, em
código** (a ordem em camadas: ID de hardware **e** compatível > classe > sem regra = permitido). Um nó é bloqueado se ele **ou qualquer pai** for bloqueado.
Só aplica se **todos** estes ficarem permitidos, com todos os pais:

- teclado, mouse, touchpad e demais HID **de qualquer barramento** (USB, Bluetooth, PS/2, I2C/ACPI);
- discos, controladores de disco (NVMe/SATA/RAID) e controladores USB e hubs raiz **internos**; e ao menos um disco interno precisa aparecer na leitura;
- composto: **todos** os nós (`&MI_xx`) do dispositivo USB que contém um essencial;
- a cadeia de pais (por exemplo, o rádio Bluetooth USB que leva o teclado Bluetooth).

**Teclado, mouse ou outro HID conectado e DESMARCADO = recusa por regra explícita**, qualquer que seja a classe do nó USB (mesmo que a política não o bloqueasse),
porque o toolkit não aceita deixar um essencial fora da lista.

Leitura que falha, JSON cortado, contagem que não bate, pai desconhecido, ciclo, instância repetida ou ID com caractere de controle = **recusa**, nada é
alterado. **Teclado ou mouse conectado e desmarcado = recusa** (o plano diz qual). Desmarcar um pendrive (nó de classe `USB`) é permitido: ele fica bloqueado ao reconectar. Para um dispositivo desmarcado cuja classe a política não nega, o
plano diz que ele ficou fora da lista mas que a política **não o bloquearia**.

## Ordem obrigatória do `aplicar`

1. prazo (2..60) -> edição (Pro, Enterprise, Education) -> **administrador** (recusa antes de qualquer mudança; o toolkit nunca eleva sozinho) -> nenhum backup
   nem tarefa anteriores;
2. plano com a **prova**, recalculado dentro do `Apply` e comparado, texto a texto, com o plano que foi MOSTRADO e confirmado (`ApplyOptions.ExpectedPlan`;
   diferente = `ErrPlanChanged`, antes de qualquer passo): o que é gravado é o que foi mostrado;
3. pasta protegida `C:\ProgramData\toolkit` (caminho FIXO, nunca a variável `%ProgramData%`): criada, **dono Administradores**, ACL só de SYSTEM e
   Administradores (sem herança), ponto de reparse recusado, ACL **relida em SDDL** (não depende do idioma) e conferida;
4. **backup**: `usb-backup.json` (atômico) com SHA-256 do conteúdo, de cada alvo da lista fechada (inclusive "não existia"), e `usb-restrictions-backup.reg`
   (com hash) quando a chave já existia; relido e comparado com o gravado;
5. cópia do binário na pasta protegida (hash da cópia = hash do original);
6. **tarefa de reversão** criada (SYSTEM, execução única, `StartWhenAvailable`, comando fixo) e **conferida**: existe, habilitada, programa e argumento **exatos**,
   SYSTEM, próximo disparo no futuro e dentro do prazo (com folga de 2 min);
7. **só então** as chaves, na ordem segura: `AllowDenyLayered` primeiro (sem ela a negação venceria a liberação), a lista de IDs, e a negação **por último**;
8. releitura: cada entrada tem de estar como gravada.

Falha em qualquer passo antes da primeira chave = **zero alterações** (a tarefa e o backup deste `aplicar` são removidos). Falha no meio das chaves: tentativa
de **restauração imediata**; se ela também falhar, a tarefa e o backup ficam como reserva.

## Reversão automática e `confirmar`

Sem `toolkit usb confirmar` dentro do prazo, a tarefa executa `toolkit usb reverter` (como SYSTEM, sem sessão) e restaura o snapshot sozinha. `confirmar`
(ou a tecla `n` na TUI, com confirmação própria e foco em Cancelar) confere que a tarefa é **exatamente** a do toolkit, remove só ela e mantém a política e o
backup. Viabilidade (tarefa rodando sem sessão e com o teclado bloqueado, depois de reiniciar, ACL, comportamento se a máquina desligar antes do prazo):
**A VERIFICAR em VM**.

## Desfazer garantido

- Restaura **exatamente o snapshot validado** (`usb-backup.json`: hash, estrutura, caminhos só da lista fechada, tamanho limitado). **Nunca apaga a chave
  `Restrictions` às cegas**: só remove o que o toolkit criou (valores, subchaves `AllowDeviceIDs`/`DenyDeviceClasses` **vazias** e, se o snapshot registra que
  ela não existia e está vazia, a própria `Restrictions`). Política de outra origem que apareça depois **fica**.
- Valor que o toolkit gravou e que agora tem outro conteúdo = **conflito**: nada é escrito.
- **Snapshot ausente, corrompido ou adulterado depois de uma aplicação** (a tarefa de reversão existe, ou há chaves da lista fechada sem snapshot): o toolkit
  diz **ESTADO ORIGINAL DESCONHECIDO**, **não escreve nada** e mostra a recuperação manual. "Nada a desfazer" só aparece quando nunca houve aplicação.
- O hash detecta **corrupção**; quem impede **adulteração** é a ACL da pasta protegida (quem pode reescrever o arquivo com hash novo já é administrador).

## Se o teclado travar mesmo assim (roteiro de recuperação)

Mostrado também na confirmação e no plano. **Comandos exatos do WinRE e do Modo Seguro: A VERIFICAR em VM.**

1. Espere o prazo: a tarefa de reversão restaura sozinha.
2. Use outro teclado ou mouse **já permitido** (tenha um de reserva).
3. Entre com **outra conta de administrador**, restaure o `.reg` íntegro de `C:\ProgramData\toolkit`. **Só apague** a chave
   `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions` (Editor de Registro ou `gpedit.msc`) **depois de confirmar que não havia política
   anterior** (GPO de domínio, MDM ou outra ferramenta); senão apagaria uma política que não é do toolkit. Sem o `.reg` íntegro, o toolkit não tem como saber o
   estado original: confira com quem administra a máquina.
4. **Modo Seguro** (se a política também vale nele: A VERIFICAR).
5. **Ambiente de Recuperação do Windows (WinRE)**: prompt de comando, `reg load` do hive `SOFTWARE` offline do Windows instalado, restaurar o `.reg` íntegro ou
   (só se confirmado que não havia política anterior) apagar a chave de `Restrictions`, `reg unload`. **A VERIFICAR em VM.**

## TUI

Área `3 Segurança`, seção **USB permitidos (experimental)** (o rótulo fica no título). Início: objetivo "Permitir só os USB conhecidos (modo empresa) —
experimental", etiqueta `! só Windows` fora do Windows e `! experimental · pede administrador` (ou `! experimental`, já como administrador) no Windows.

- Ao abrir a TUI a seção **não** lê nada ("ainda não lido"; tecla `c` lê). Lista dos USB conectados (nome, VID:PID, classe, "essencial"); `espaço` marca/desmarca; `p` plano e prova (só leitura); `a` aplicar… (confirmação única com a
  evidência: lista, permitido/bloqueado, risco de trancar, backup, prazo, recuperação; foco em Cancelar; sem administrador o botão fica indisponível);
  `n` manter… (confirmação própria); `d` desfazer…; `c` ler de novo.
- Estados: carregando, erro (sem negar alteração depois de uma ação), fora do Windows, edição sem suporte (Home), **aplicado aguardando confirmação** (prazo
  estático "reverte às HH:MM", sem contagem viva: `TOOLKIT_REDUCE_MOTION` e goldens determinísticos), aplicado e confirmado, sobra, **estado original
  desconhecido**, plano recusado (nunca vira confirmação).
- Plano de uso único por ID (como a Privacidade): a confirmação só age sobre o plano exibido; Esc cancela a preparação; a ação ainda exige que o plano
  recalculado seja idêntico ao mostrado.

## O que NUNCA faz

Eleva sozinho; escreve fora de `...\DeviceInstall\Restrictions`; usa `DenyUnspecified`, classe ampla ou a opção retroativa; sobrescreve política de outra
origem; aplica sem a prova, sem o backup protegido ou sem a tarefa conferida; restaura às cegas; usa `-Force` ao criar a tarefa; mexe em qualquer outra tarefa.

## A VERIFICAR em VM (nada disto foi confirmado numa máquina real)

1. Se o registro local de `DeviceInstall\Restrictions` funciona na edição Home (o toolkit recusa de qualquer forma).
2. Formato exato da lista numerada (`AllowDeviceIDs\1`...) e dos valores de ligação (`AllowDenyLayered`, `AllowDeviceIDs`, `DenyDeviceClasses` = DWORD 1) no
   registro; todos os IDs de hardware dos nós de um teclado composto real.
3. **Se as duas classes de USB bastam para impedir um dispositivo HID novo e simples (nó de classe `HIDClass`) e o filho HID de um composto**; efeito sem
   `gpupdate`/reinício; se vale no Modo Seguro. (Se não bastarem, a função só barra dispositivos de classe `USB`/`USBDevice`, como armazenamento em massa.)
4. `Get-PnpDevice`/`Get-PnpDeviceProperty` (`DEVPKEY_Device_Parent`): disponibilidade e formato por versão do Windows; leitura completa da árvore (teclado I2C,
   PS/2, Bluetooth, NVMe/SATA, composto); codificação da saída do PowerShell.
5. Tarefa agendada: executar sem sessão e com o teclado bloqueado, depois de reboot, `Register-ScheduledTask` com `S-1-5-18`/`ServiceAccount`, a tarefa se
   remover enquanto roda, `Get-ScheduledTask`/`Get-ScheduledTaskInfo` (campos, `NextRunTime`) lidos por conta não administradora, comportamento se a máquina
   desligar antes do prazo.
6. ACL da pasta protegida: `Set-Acl` com dono Administradores, o SDDL exato que o Windows devolve (`O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)` é o esperado),
   criação prévia da pasta por usuário comum.
7. Saída de `reg query /s` e `reg export` (formato, tipos, nomes de valor com espaços, `(Default)`), por idioma.
8. Detecção de administrador quando o processo roda como SYSTEM (a tarefa de reversão).
9. WinRE e Modo Seguro: comandos do roteiro de recuperação.
10. Estabilidade do ID de instância (decisão 6: hoje só ID de hardware).

## Testes

`internal/usb` (fixtures SINTÉTICAS: `VID_FFFF`/`PID_0001`, nomes inventados; nenhum ID real): árvore e prova (teclado I2C, PS/2, Bluetooth, NVMe, SATA, hub
raiz, composto, árvore incompleta, ciclo), teclado/mouse desmarcado, ordem do aplicar (falha em cada passo = zero chaves), snapshot restaurado exatamente,
outras origens preservadas, snapshot corrompido/ausente, Home, sem administrador, prazo, confirmar, e o **contrato dos comandos** sobre um Windows simulado
(HKLM só em `Restrictions`, tarefa só a do toolkit, nenhum `Elevate`, scripts ASCII). `internal/cli` (comandos, revalidação depois do `[s/N]`, backend da TUI) e
`internal/tui` (estados, plano por ID, goldens 80x24, 120x40 e 60x16).
