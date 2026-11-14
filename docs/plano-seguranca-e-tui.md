# Plano: segurança de dispositivos, rede, otimização e mais apps (com TUI)

> **Estado: PROPOSTA para decisão do Paulo. Nenhum código foi escrito.** Este documento só planeja. Cada função vira um PR pequeno, na ordem da seção [Ordem dos PRs](#ordem-dos-prs).
> Fatos técnicos foram conferidos nas fontes oficiais listadas em [Fontes](#fontes) (consultadas em 2026-11-14). O que não deu para confirmar está marcado
> **A VERIFICAR** (a lista consolidada está em [Pendências A VERIFICAR](#pendências-a-verificar)). Nenhum id de pacote foi inventado: os que não foram
> conferidos estão como "a confirmar".

## Índice

1. [Resumo executivo](#resumo-executivo)
2. [Regras que valem para tudo](#regras-que-valem-para-tudo)
3. [Função 1: modo empresa, USB permitidos](#função-1-modo-empresa-usb-permitidos)
4. [Função 2: análise de rede (só leitura)](#função-2-análise-de-rede-só-leitura)
5. [Função 3: otimização do Windows](#função-3-otimização-do-windows)
6. [Função 4: mais instalações no catálogo](#função-4-mais-instalações-no-catálogo)
7. [Riscos transversais e segurança do plano](#riscos-transversais-e-segurança-do-plano)
8. [Ordem dos PRs](#ordem-dos-prs)
9. [Decisões abertas](#decisões-abertas)
10. [Pendências A VERIFICAR](#pendências-a-verificar)
11. [Fontes](#fontes)

## Resumo executivo

1. Quatro funções novas, todas também na TUI (regra do Paulo): USB permitidos (Segurança), análise de rede (Rede), otimização do Windows (Máquina) e mais apps no catálogo (Instalar).
2. Mesmo padrão dos PRs de privacidade e apps nativos: plano antes, confirmação com foco em Cancelar, backup e desfazer, administrador explícito (nunca eleva sozinho), `--dry-run`, catálogo fechado, testes com fakes.
3. Ordem proposta: rede (só leitura) primeiro, depois apps, otimização (leitura), otimização (animações) e USB por último, por ser a função de maior risco (pode trancar o teclado).
4. **USB: limite honesto.** Um bad USB que clona o VID/PID de um teclado permitido passa. A lista barra dispositivo desconhecido e armazenamento pessoal; não é defesa contra quem tem acesso físico e conhece os IDs.
5. USB no Windows usa a política oficial de restrição de instalação de dispositivos (chaves em `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions`). Ela só impede **instalar ou atualizar driver** de dispositivo novo: o que já está instalado continua funcionando.
6. USB tem reversão automática por tarefa agendada até o usuário confirmar, e só aplica depois de **provar** pelo PnP que todo dispositivo essencial (teclado, mouse, touchpad, disco do sistema e seus pais, de qualquer barramento) continua permitido, e só com a reversão **armada e conferida** antes da primeira restrição; a reversão restaura o snapshot validado, não apaga chaves. Viabilidade a provar numa VM (A VERIFICAR).
7. Rede é leitura pura. IP público **só sob pedido** (faz uma requisição a serviço externo); nada de varredura de terceiros; nada muda.
8. Otimização lê gargalos (CPU, RAM, disco, inicialização, serviços) e só muda as animações, por usuário (`HKCU`), com backup e desfazer. Nada de desligar serviço de segurança nem "otimizador" agressivo.
9. Apps novos entram no catálogo fechado por PR, só com id verificável. Recomendado: Brave, Chrome, LibreOffice, PeaZip e RustDesk (com ressalvas). Não entram: TightVNC/UltraVNC para uso pela internet (sem criptografia de sessão na versão livre) e acesso remoto proprietário sem garantia de gratuidade vitalícia.
10. Decisões que o Paulo precisa tomar antes estão em [Decisões abertas](#decisões-abertas), cada uma com a recomendação.

## Regras que valem para tudo

Herdadas do projeto (`CLAUDE.md`, `docs/privacidade.md`, `docs/apps-nativos.md`, `docs/diagnostico.md`):

- Nunca executar nada real no Windows, Linux ou macOS da máquina de desenvolvimento: `exec.Runner` mockado (`exectest.FakeRunner`) ou `--dry-run`. Smoke real só em VM (`docs/smoke.md`).
- Toda leitura leva `ReadOnly: true` e passa pelo `exec.ReadOnlyRunner`; toda mudança passa pelo `MutatingRunner`. Caminho absoluto para executáveis do sistema.
- O plano é sempre impresso antes. `--dry-run` para no plano. Sem `--dry-run`, `[s/N]` com foco em Cancelar; sem terminal exige `--yes`.
- Administrador é **explícito**: o comando recusa **antes de mudar qualquer coisa** e manda abrir o terminal como administrador. O toolkit nunca pede elevação sozinho (`Deps.Admin`, `Skipped.NeedsAdmin`).
- Backup do estado original (0600, escrita atômica, tratado como dado não confiável e conferido contra o catálogo antes de restaurar) e `desfazer`.
- Catálogo fechado: cada chave, serviço ou comando vem de uma lista no código; nada digitado pelo usuário vira comando. Todo texto lido do sistema passa por `textsafe`.
- Toda função nova entra na TUI **no mesmo PR** (seção em `internal/tui`, estados padronizados de `stateview.go`, faixa de resultado de `result.go`, ajuda e Início atualizados, strings em pt-BR/en/es em `internal/msg`, acessibilidade conforme `docs/auditoria-a11y-l10n.md`).
- Um PR = uma fase pequena, com testes, docs, `scripts/ci.sh` verde, `security-audit` em modo guia (linha "Segurança" no relatório) e revisão de PR.
- Repositório público: nada de dado de cliente, segredo, IP, e-mail nem referência a projeto privado.

---

## Função 1: modo empresa, USB permitidos

**Objetivo.** Gravar os dispositivos USB conectados hoje como "permitidos" e bloquear qualquer outro (contra bad USB e pendrive pessoal).

> ### LIMITE HONESTO (leia antes de decidir)
>
> - **Um bad USB que clona o VID/PID de um teclado permitido PASSA.** Os IDs são anunciados pelo próprio dispositivo; quem os conhece (ou os lê de um teclado da empresa) fabrica um dispositivo com o mesmo ID.
> - A lista barra principalmente **dispositivo desconhecido** e **armazenamento pessoal** (o pendrive que alguém trouxe de casa, o cabo de carga que esconde um teclado de modelo diferente).
> - **Não é defesa contra atacante com acesso físico e conhecimento dos IDs.** Quem precisa dessa defesa precisa de controle de endpoint gerenciado (MDM/EDR), bloqueio físico das portas e política de segurança, fora do escopo do toolkit.
> - No Windows a política só controla a **instalação de driver** de dispositivo novo; dispositivo já instalado continua funcionando (ver abaixo).
> - Mensagem do toolkit na TUI e no CLI: "Isto reduz o risco, não o elimina."

### Windows

**Mecanismo oficial.** Política "Restrições de instalação de dispositivos" (Group Policy local ou registro). Fonte: [1], [2].

| Política (nome em inglês) | Valor de registro em `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions` [2] | Uso aqui |
|---|---|---|
| Allow installation of devices that match any of these device IDs | `AllowDeviceIDs` | lista de IDs de hardware permitidos |
| Allow installation of devices that match any of these device instance IDs | `AllowInstanceIDs` | alternativa mais estrita (por instância) |
| Allow installation of devices using drivers that match these device setup classes | `AllowDeviceClasses` | não usar para liberar classe inteira |
| Prevent installation of devices not described by other policy settings | `DenyUnspecified` | bloqueia o que não está descrito |
| Apply layered order of evaluation for Allow and Prevent policies | `AllowDenyLayered` | recomendado pela Microsoft no lugar de `DenyUnspecified` [2] |
| Prevent installation of devices that match any of these device IDs / instance IDs / setup classes | `DenyDeviceIDs` / `DenyInstanceIDs` / `DenyDeviceClasses` | bloqueios explícitos (com opção "retroativa") |

Pontos confirmados nas fontes [1], [2]:

- As políticas de lista valem por **ID de hardware ou ID compatível**. A lista "Allow" só faz sentido com `DenyUnspecified` ligado (modo legado) ou com `AllowDenyLayered` ligado (ordem em camadas: instância > ID do dispositivo > classe de configuração > removíveis). A Microsoft diz que a camada **substitui** o `DenyUnspecified` e recomenda usá-la quando possível [2].
- Com a camada **desligada**, toda política "Prevent..." tem precedência sobre qualquer "Allow" [1], [2]. Com ela ligada, a mais específica vence.
- A política afeta **instalar ou atualizar driver**. As políticas "Prevent" aceitam aplicar também a dispositivos **já instalados** (opção "retroativa"); `DenyUnspecified` e as listas Allow não têm essa opção [1], [2]. Ou seja: **um pendrive que alguém já usou nesta máquina antes de aplicar a política continua funcionando**, a menos que se bloqueie retroativamente por classe ou ID.
- A política vale por máquina (escopo Device) e o doc da Microsoft para `DeviceInstallation` lista as edições **Pro, Enterprise, Education e IoT Enterprise**; **Home não aparece na lista** [2]. Se o registro local funciona na Home mesmo sem o suporte oficial, **A VERIFICAR** na VM. Decisão recomendada: no Home, o toolkit **recusa** com mensagem clara (não promete o que a Microsoft não documenta).
- Para dispositivo com funções lógicas (composto), é preciso permitir **todas** as strings de identificação, senão há efeito inesperado [1]. Um teclado USB costuma ter vários nós filhos (`USB\VID_xxxx&PID_xxxx`, interfaces `&MI_00`, nós `HID\...`). O plano precisa listar todos os IDs de hardware de cada nó da árvore do dispositivo conectado. Formato exato: **A VERIFICAR** em VM real.
- **Cuidado com ID compatível genérico.** A lista casa também por ID compatível. Colocar na lista um ID compatível genérico (por exemplo uma classe HID geral) liberaria todo dispositivo daquela classe, incluindo o bad USB. A regra do toolkit: **só IDs de hardware específicos (com VID e PID)**; ID compatível genérico nunca entra (teste de regressão).
- **`DenyUnspecified` e a lista "permitir só os IDs" são GLOBAIS: afetam todos os barramentos, não só USB.** Bluetooth, PS/2, I2C, PCI/NVMe/SATA, monitor, impressora, dock e placa nova também deixam de instalar o que não estiver na lista [2]. Sozinhos, portanto, só podem ser usados se o toolkit **provar** (seção "Prova de dispositivos essenciais", abaixo) que todo dispositivo essencial e seus pais ficam permitidos; senão o comando **recusa e não aplica nada**.
- **Escopo do bloqueio: por ID e por barramento USB, nunca por classe ampla.** Bloquear por classe de configuração `Keyboard`, `Mouse`, `HIDClass` ou `DiskDrive` **não se limita a USB**: atinge o teclado interno do notebook, teclado/mouse Bluetooth, touchpad I2C/PS2, o disco interno (NVMe/SATA) e os dispositivos pais desses, e a própria Microsoft avisa que bloquear a classe `DiskDrive` de forma retroativa pode deixar o disco interno inutilizável e quebrar a máquina [2]. Por isso essas classes **ficam fora do plano** (teste de regressão: o plano nunca contém classe de entrada ou armazenamento). O desenho é a **ordem em camadas** (`AllowDenyLayered`; instância > ID do dispositivo > classe > removíveis [2]) com a **negação limitada ao hardware do barramento USB**: o cenário de USB da fonte [2] bloqueia só as duas classes de USB (`{36fc9e60-c465-11cf-8056-444553540000}` e `{88BAE032-5A81-49f0-BC3D-A4FF138216D6}`) e libera por ID os controladores, hubs raiz, hubs genéricos e os dispositivos desejados; nomes das duas classes e se bastam para impedir o filho HID de um dispositivo novo: **A VERIFICAR** na VM. Variante mais estreita, também por confirmar: a camada de "dispositivos removíveis" (nome exato do valor de registro: **A VERIFICAR**). Hubs e controladores entram na lista pelos IDs genéricos que a fonte cita (`PCI\CC_0C03`, `PCI\CC_0C0330`, `PNP0CA1`, `USB\ROOT_HUB20`, `USB\ROOT_HUB30`, `USB\USB20_HUB`) e, em desktops, a fonte alerta que é preciso listar todos os USB pelos quais teclado e mouse se ligam, senão o usuário perde o acesso por HID [2].

**Como ler os IDs dos dispositivos conectados (só leitura, sem administrador).**

- PowerShell: `Get-PnpDevice -PresentOnly` e, por dispositivo, `Get-PnpDeviceProperty -KeyName DEVPKEY_Device_HardwareIds, DEVPKEY_Device_CompatibleIds, DEVPKEY_Device_InstanceId, DEVPKEY_Device_Class`. Executável por caminho absoluto (`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`), script de texto fixo, saída em JSON. Disponibilidade dos cmdlets e formato: **A VERIFICAR** (PowerShell 5.1 no Windows 10/11).
- `pnputil /enum-devices /connected /ids` (a Microsoft cita o PnPUtil como ferramenta para ver IDs de hardware e compatíveis [2]; o nome exato das opções por versão: **A VERIFICAR**).
- Saída é dado não confiável: `textsafe`, tamanho limitado, validação de formato (`^[A-Z0-9_\\&\-\.{}]+$`) antes de entrar no plano.

**Não trancar teclado, mouse nem hub.**

1. Classificar cada dispositivo conectado por classe e **barramento** (teclado, mouse, hub, armazenamento, outros; USB, Bluetooth, PS/2, I2C, PCI). Teclado e mouse **conectados** que não ficariam permitidos (por falta de ID legível, composto mal lido, ou pai fora da lista) fazem o comando **recusar** ("o teclado X ficaria fora da lista"), qualquer que seja o barramento.
2. Hubs e controladores USB já instalados continuam (a política não remove o que está instalado). Mas **incluir os hubs presentes** na lista, para não bloquear reinstalação.
3. Teclado/mouse **não USB** (notebook com teclado interno, Bluetooth, touchpad I2C/PS2) **não podem ser afetados**, e o toolkit tem de **provar** isso (seção seguinte), não presumir: o plano os mostra como "não afetado" com a evidência (barramento e cadeia de pais).
4. Verificação **antes de aplicar**: o plano mostra, dispositivo a dispositivo, "permitido" ou "bloqueado se reconectar", e a lista exata de IDs que será gravada.
5. Recomendar fortemente ter **um segundo teclado/mouse de reserva** de modelo já na lista, e o desfazer testado (abaixo).

**Prova de dispositivos essenciais (condição para aplicar).** Antes de gravar qualquer chave, o toolkit lê o PnP (só leitura: `Get-PnpDevice -PresentOnly` e `Get-PnpDeviceProperty` com `DEVPKEY_Device_Parent`/`DEVPKEY_Device_InstanceId`, e `pnputil /enum-devices /connected /relations` como segunda fonte; nomes e formato por versão: **A VERIFICAR**) e monta a **árvore de pais** de cada dispositivo presente. Depois **avalia a política candidata contra essa árvore, em código** (a mesma ordem de camadas: instância > ID > classe > removíveis) e só aplica se **todos** estes ficarem **permitidos**, juntos com todos os seus pais até a raiz:

- teclado, mouse, touchpad e demais HID **de qualquer barramento** (USB, Bluetooth, PS/2, I2C/ACPI);
- o disco de sistema e seus controladores (NVMe, SATA/AHCI, RAID, e o barramento PCI deles) e o volume de boot;
- controladores e hubs raiz USB e os hubs que ligam teclado e mouse conectados;
- dispositivos compostos: **todos** os nós filhos e a interface de cada função (`&MI_xx`), não só o nó pai [1].

Se não for possível **provar** (leitura falhou, árvore incompleta, pai desconhecido, sem administrador para ler), a resposta é **recusar com mensagem** (qual dispositivo não foi provado) e **não aplicar nada**; falha de leitura nunca vira "provavelmente seguro". O plano mostra a prova (dispositivo, barramento, cadeia de pais, "permitido por: ID/camada") antes do `[s/N]`.


**Efeito, reinício e comportamento.** A política vale na próxima tentativa de instalar ou atualizar driver; para confirmar o efeito, a Microsoft indica olhar `C:\Windows\INF\setupapi.dev.log` à procura da seção `[Device Installation Restrictions Policy Check]` [2]. Se é preciso reiniciar para um dispositivo já instalado deixar de funcionar nos casos "retroativos": o doc dos cenários menciona reiniciar a máquina antes de conferir [1]. Se o registro local aplica **sem** `gpupdate`: **A VERIFICAR**. O toolkit não prometerá "agora" sem testar na VM.

**Prazo/confirmação: aplicar com timer de reversão (armado e conferido ANTES de restringir).** Proposta:

1. **Ordem obrigatória do `aplicar`:** (a) prova de dispositivos essenciais (acima); (b) **backup protegido** do estado atual; (c) **criar a tarefa de reversão**; (d) **conferir a tarefa**; só então (e) gravar a primeira chave de restrição. Falha em qualquer passo de (a) a (d) = **ZERO alteração**: o comando aborta **antes de tocar no registro** e informa o motivo.
2. **Backup protegido:** `usb-backup.json` e o `.reg` exportado ficam em `C:\ProgramData\toolkit\` com ACL restrita a **SYSTEM e Administradores** (herança desligada; nenhuma ACE para `Users`/`Authenticated Users`/`Everyone`), em pasta que usuário comum não escreve; a ACL é **lida de volta e conferida** depois de gravar (ACL ampla = aborta). Cada arquivo leva o hash SHA-256 (ver "Desfazer garantido").
3. **Tarefa de reversão** (conta do sistema, execução única, daqui a N minutos, padrão 10; também "executar assim que possível" se o horário passar). Comando **fixo**, que só restaura o snapshot validado do backup (nunca apaga as chaves às cegas). **Conferência antes de restringir:** a tarefa existe; está **habilitada**; o binário ou script do comando está em local com **ACL protegida** (SYSTEM/Administradores; senão a tarefa viraria escalonamento de privilégio); o comando registrado é **exatamente** o esperado; e o **próximo disparo** é válido (no futuro, dentro do prazo). Os cmdlets e campos para ler isso (`Get-ScheduledTask`, `Get-ScheduledTaskInfo`, `NextRunTime`): **A VERIFICAR** na VM.
4. O usuário executa `toolkit usb confirmar` (ou o botão "Manter" na TUI) dentro do prazo; isso remove a tarefa. Sem confirmação, a tarefa restaura sozinha.
5. Viabilidade e limites (A VERIFICAR em VM): a tarefa roda mesmo sem sessão e com o teclado bloqueado (é o ponto dela); depende do serviço Agendador ativo; se a máquina desligar antes do prazo, a tarefa roda no próximo boot ou a política já fica aplicada. Alternativa que evita depender do binário: a tarefa executa um script PowerShell de texto fixo embutido (também com ACL protegida) que faz a restauração validada.
6. A tarefa é mostrada **no plano** (nome, horário, comando exato) antes do `[s/N]`. Ela é criada pelo comando que o administrador já iniciou: isso **não** é elevar sozinho.

**Desfazer garantido.**

- **Snapshot do estado original** de **cada valor** da pasta `Restrictions` (inclusive "não existia") **e de qualquer chave/valor pré-existente**, inclusive políticas de **outras origens** (GPO de domínio, MDM, outra ferramenta), em `usb-backup.json` (atômico) com **hash SHA-256** do conteúdo e do `.reg` exportado de segurança, ambos em `C:\ProgramData\toolkit` com a ACL protegida acima. Aplicar duas vezes nunca sobrescreve o original.
- **Validação antes de restaurar:** o arquivo de backup tem de estar na pasta protegida (ACL conferida), com o hash batendo, o esquema/estrutura válidos (chaves dentro de `...\DeviceInstall\Restrictions`, nomes e tipos de valor da lista fechada, tamanho limitado) e consistente com o `.reg`. Só então restaura.
- `toolkit usb desfazer [--dry-run] [--yes]` e a **tarefa de reversão** fazem a mesma coisa: **restauram EXATAMENTE o snapshot validado**, preservando o que já existia antes (valores, listas e chaves de outras origens). Só removem o que **o toolkit criou** e que o snapshot registra como "não existia"; **nunca apagam a chave `Restrictions` inteira às cegas**. Depois removem a tarefa de reversão.
- **Snapshot corrompido, adulterado ou ausente:** a restauração **não roda às cegas**. Nada é escrito no registro; o comando e a tarefa param com mensagem clara, mantêm o arquivo para análise e mostram a **instrução de recuperação manual** (a mesma da lista abaixo, usando o `.reg` exportado se ele estiver íntegro, ou a remoção manual da chave `Restrictions` quando o administrador confirmar que não havia política anterior). Se o hash do `.reg` falhar também, o toolkit diz que não tem como saber o estado original e orienta a conferir com quem administra a máquina (por exemplo, GPO de domínio) antes de apagar qualquer coisa.
- **Se o teclado travar mesmo assim:** (1) esperar o timer de reversão; (2) outro teclado já permitido; (3) outra conta de administrador restaura o `.reg` íntegro; **só apaga** a chave `HKLM\SOFTWARE\Policies\Microsoft\Windows\DeviceInstall\Restrictions` (Editor de Registro ou `gpedit.msc`) **depois de confirmar que não havia política anterior** (GPO de domínio, MDM ou outra ferramenta), senão apagaria uma política que não é do toolkit; (4) Modo Seguro (se a política é aplicada também no Modo Seguro: **A VERIFICAR**); (5) Ambiente de Recuperação do Windows (WinRE): prompt de comando, `reg load` do hive `SOFTWARE` offline do Windows instalado, restaurar o `.reg` íntegro ou — **só se confirmado que não havia política anterior** — apagar a chave de `Restrictions`, `reg unload`. Esse roteiro vai em `docs/usb.md` com os comandos **conferidos na VM** (A VERIFICAR) e é mostrado na tela de confirmação.
- O que **não** é desfazível automaticamente: nada, **desde que** o snapshot exista. Sem snapshot, o `desfazer` distingue dois casos: se **nunca houve aplicação** (nenhum registro de que o toolkit aplicou), diz "nada a desfazer" (mesmo contrato da privacidade); se **houve aplicação e o snapshot sumiu** (sinais: a tarefa de reversão existe ou as chaves de `Restrictions` da lista fechada estão presentes sem snapshot correspondente), NÃO diz "nada a desfazer": diz **"estado original desconhecido"**, não escreve nada e mostra a recuperação manual acima (com a condição de confirmar que não havia política anterior antes de apagar qualquer chave).

**Administrador.** Precisa (escreve em `HKLM` e cria tarefa do sistema). O `aplicar` recusa antes de qualquer mudança se não for administrador; leitura e plano funcionam sem administrador (lista de dispositivos pode vir incompleta sem admin: `Skipped.NeedsAdmin`).

### Linux (USBGuard)

Fontes: [3], [4], [5].

- **O que é:** daemon que autoriza ou bloqueia dispositivos USB por regras; licença GPL-2.0-ou-posterior [3]. Pacote `usbguard` no Debian, Ubuntu (universe) e Fedora [5]. No Arch: não está confirmado no repositório oficial (o que se encontrou foi o AUR), **A VERIFICAR**; sem pacote verificado no repositório oficial, o toolkit não instala por pacman.
- **Política inicial:** `usbguard generate-policy` gera as regras para os dispositivos conectados agora; o README manda gravar em `/etc/usbguard/rules.conf` **antes de iniciar o serviço**, para manter teclado e mouse funcionando [3].
- **Padrões do daemon** (`usbguard-daemon.conf`) [4]: `ImplicitPolicyTarget` (dispositivo que não casa com regra) padrão `block`; `PresentDevicePolicy` (já conectados quando o daemon inicia) padrão `apply-policy`; `InsertedDevicePolicy` (conectados depois) padrão `apply-policy`; `PresentControllerPolicy` padrão `keep`; `IPCAllowedUsers` padrão `root`.
- **Serviço:** `systemctl start|enable usbguard.service` [3]. Pelo gerenciador do SO o unit já vem do pacote (confirmar por distro, **A VERIFICAR**).
- **Risco de trancar o teclado na sessão:** se o `rules.conf` não incluir o teclado conectado (por exemplo teclado trocado depois), o dispositivo é bloqueado ao ser inserido. Mitigações iguais às do Windows: recusar se algum teclado/mouse conectado não estiver nas regras geradas, tempo de reversão (`systemd-run --on-active=10m` com comando fixo que **restaura o estado anterior registrado** (ver Desfazer), nunca um `stop` cego; viabilidade **A VERIFICAR**) e, quando o USBGuard não existia antes, não habilitar no boot até a confirmação (`start` agora, `enable` só em `confirmar`).
- **Liberar na hora:** `usbguard list-devices` e `usbguard allow-device <id>` (nomes dos subcomandos vêm da documentação do USBGuard; formato de saída e opção `-p` para permanente: **A VERIFICAR** na página de manual, não coberta pelas fontes lidas).
- **Instalação:** via gerenciador do SO por `internal/providers` (apt/dnf), com `Elevate` (`sudo -n`), nunca por script de terceiros. Sem pacote no gerenciador = a função mostra "indisponível nesta distribuição".
- **Permissões:** `rules.conf` com dono root e permissão restrita (conferir a que o pacote cria, **A VERIFICAR**); o toolkit não amplia `IPCAllowedUsers`.
- **Desfazer (restaura o estado anterior; nunca para nem desabilita um USBGuard que JÁ EXISTIA).** **Antes de qualquer mudança** o toolkit registra, em `/var/lib/toolkit` (root, 0600, com hash SHA-256): (a) se o USBGuard **já estava instalado** (o toolkit o instalou ou não); (b) o estado do serviço: `active`/`inactive` e `enabled`/`disabled` (leitura por `systemctl is-active` e `is-enabled`, só leitura); (c) as **regras**: `/etc/usbguard/rules.conf` e **todo o diretório `rules.d`** (conteúdo, **permissões e dono** de cada arquivo); (d) a **configuração** `usbguard-daemon.conf` (conteúdo, permissões e dono); (e) a **política anterior** em uso (conteúdo e permissões/dono). O `desfazer`, depois de conferir os hashes do registro (adulterado = recusa, sem escrever, com instrução manual):
  - **se havia USBGuard:** devolve arquivos, permissões e dono **exatos**, e leva o serviço ao estado anterior (se estava `active`+`enabled`, continua assim, apenas recarregando a política restaurada; se estava `inactive`/`disabled`, volta a isso). **Não** faz `disable --now` num serviço que já rodava;
  - **se não havia:** remove **só o que o toolkit criou** (as regras que ele gravou) e para/desabilita **só o serviço que ele próprio iniciou/habilitou** (o registro diz quais); a remoção do pacote, se o toolkit o instalou, **só se o usuário pedir** (não remove pacote sozinho).
- **Administrador:** sim (`sudo -n`); sem credencial = `ErrNeedsAdmin` e dica `sudo -v`, como no `install`.
- Mesma ressalva honesta de clonagem de VID/PID: o USBGuard identifica por atributos que o dispositivo declara (inclusive hash do descritor, o que dificulta mas não impede a falsificação por dispositivo programável; detalhe do hash: **A VERIFICAR**).

### macOS (sem MDM)

Fonte: [6].

- O que dá: em **notebooks Mac com Apple silicon**, o macOS pergunta antes de aceitar um acessório USB, Thunderbolt ou cartão SD novo. Ajuste em **Ajustes do Sistema > Privacidade e Segurança > Permitir que acessórios se conectem**, com as opções (nomes em inglês na fonte): "Always ask", "Ask for new accessories" (padrão), "Automatically allow when unlocked", "Always allow" [6]. "Don't Allow" bloqueia o reconhecimento do acessório e dos dados, mas ele ainda pode carregar o Mac [6]. É preciso desbloquear o Mac para aprovar [6].
- O que **não** dá: o toolkit **não tem como mudar essa opção** sem MDM/Apple Configurator (a fonte diz que só em modelos supervisionados, por administradores, com Apple Configurator [6]) e **não há lista de IDs permitidos** como no Windows e no Linux. Desktops (iMac, Mac mini, Mac Studio) e Macs Intel: a opção da fonte é para notebooks com Apple silicon; o resto **A VERIFICAR**.
- Decisão recomendada: no macOS a receita é **só orientação e leitura**: mostra o passo a passo e o estado se puder ser lido (leitura da preferência: **A VERIFICAR**, pode não existir forma suportada). Sem mudança, sem backup, sem administrador.

### Fluxo na TUI (área 3 Segurança, seção "USB permitidos")

| Estado | Tela |
|---|---|
| Carregando | `\| Lendo os dispositivos USB…` (Windows e Linux) |
| Sem suporte | macOS: orientação (acima) e nada para executar; Windows Home: "esta edição não documenta a política" |
| Pronto | lista dos dispositivos conectados: nome, VID:PID, classe, "entra na lista" / "não afetado" / "não dá para listar" |
| Plano e confirmação | tela de revisão padrão (`confirm.go`), **foco em Cancelar**, com a **evidência**: a lista de dispositivos, o que será **permitido** e o que será **bloqueado**, o **aviso de risco de trancar o teclado**, o prazo do timer de reversão e o roteiro de recuperação. Botões: Aplicar (com timer) / Cancelar |
| Aplicado, aguardando confirmação | faixa de resultado com **contagem regressiva** e o botão Manter; sem ação = reverte |
| Confirmado | faixa "USB permitidos ativo: N dispositivos"; ação Desfazer disponível |
| Desfeito | faixa "restaurado"; a seção volta a "Pronto" |
| Recusa | mensagem com o motivo (sem administrador, teclado fora da lista, Home) e `Nada foi alterado` |

Teclas, acessibilidade e textos seguem `docs/tui.md` e `docs/auditoria-a11y-l10n.md` (nada só por cor, `TOOLKIT_REDUCE_MOTION` desliga a animação da contagem). O Início ganha o objetivo "Proteger as portas USB" ligado a essa seção.

### Testes (fakes, sem tocar em máquina real)

- Parse de saída de PnP e de `usbguard` com fixtures **sintéticas** (README explicando, como nos providers), a serem trocadas pelas saídas reais no smoke de VM.
- Plano determinístico: mesma entrada, mesma lista; recusa se teclado/mouse conectado ficaria de fora; recusa se a lista tiver ID compatível genérico; recusa Home; recusa sem administrador **antes** de qualquer escrita.
- Backup/desfazer: ida e volta idêntica (incluindo "não existia"), backup adulterado é recusado, aplicar duas vezes não perde o original, falha no meio deixa backup válido.
- Backup protegido: ACL ampla (ou pasta gravável por usuário comum) aborta antes de tocar no registro; hash do backup adulterado ou estrutura inválida = restauração recusada, sem escrita, com a instrução manual; política de outra origem pré-existente é preservada na ida e volta (nunca apagada).
- Tarefa de reversão: falha ao criar/conferir (inexistente, desabilitada, ACL do binário ampla, comando diferente, próximo disparo inválido) = **zero alteração no registro**; a conferência acontece antes da primeira chave.
- **Dispositivos essenciais nunca bloqueados (obrigatório, no fake de PnP):** teclado **I2C**, teclado **PS/2**, teclado e mouse **Bluetooth**, touchpad I2C, disco interno **NVMe** e **SATA**, e os pais de cada um; **hub raiz USB e controladores** presentes; **dispositivo composto** (todos os nós `&MI_xx` e o pai); árvore incompleta ou pai desconhecido = recusa sem aplicar; o plano nunca contém classe `Keyboard`, `Mouse`, `HIDClass` ou `DiskDrive`.
- Timer: criação e remoção da tarefa só por comando fixo; confirmar remove; expirar restaura (no fake).
- Linux: USBGuard **pré-existente** (serviço `active`+`enabled`, `rules.d` com arquivos e dono/permissão próprios) volta **idêntico** após aplicar e desfazer, sem `stop`/`disable`; sem USBGuard prévio, o desfazer remove só o criado e para só o serviço iniciado pelo toolkit; registro adulterado = recusa.
- Texto malicioso em nome de dispositivo (ESC, bidi) é neutralizado.
- TUI: golden dos estados, foco inicial em Cancelar, redimensionamento, `NO_COLOR`.

---

## Função 2: análise de rede (só leitura)

**Escopo.** Receita de leitura com **achados** (severidade) e **"o que fazer"**, no mesmo formato do diagnóstico (`docs/diagnostico.md`). **Nada muda no sistema.** Mapeamento: pacote novo `internal/rede` (coleta por SO em `coleta_win.go`/`coleta_lin.go`/`coleta_mac.go`, `analise.go`, `deps.go` injetável), reaproveitando `exec.ReadOnlyRunner`, `textsafe` e o padrão de `internal/diagnostico` (hoje o diagnóstico só lê as placas físicas e seus MACs, sem análise de rede; reaproveitar tipos, não o coletor). TUI: área 4 Rede, seção "Análise" (hoje o placeholder `StateSoon` "ainda não existe").

### Regras da função

- **IP público só sob pedido da pessoa.** Ler o IP público exige uma requisição a um serviço externo, que revela o IP de origem a terceiro. Por isso: não vai na análise padrão, só com `--ip-publico` no CLI ou o botão "Descobrir meu IP público" na TUI, que mostra antes **qual serviço** será contatado e pede confirmação. Decisão recomendada: oferecer o serviço **configurável** (`TOOLKIT_IP_URL` ou flag) e, por padrão, **nenhum** até o Paulo escolher (ver [decisões](#decisões-abertas)); sem serviço configurado, a função explica como saber o IP por outro meio (painel do roteador). Se houver serviço padrão: HTTPS, host fixo no código, sem redirect a outro host, limite de tamanho e de tempo, sem cookie, sem envio de dado (um GET só), resposta validada como IPv4/IPv6 e nada guardado em disco.
- **"Máquina exposta?" sem escanear terceiros.** Em vez de escanear a máquina de fora (o que exigiria um serviço externo sondar o IP), o toolkit cruza **portas em escuta em endereço não local** (0.0.0.0, `::`, ou o IP da LAN) com o estado do firewall e o perfil de rede. O achado diz "esta porta aceita conexões de fora desta máquina e o firewall está X", não "a internet alcança isto" (isso depende do roteador/NAT, que o toolkit não vê). Nunca há varredura de portas de outro host, nem do roteador, nem de IP público.
- Tudo local: IPs, MACs e nomes de rede são mostrados sem máscara (decisão do Paulo para ferramenta de análise, como o diagnóstico), mas **`--relatorio` e `--json` avisam que contêm dados da rede local antes de gravar** (privacidade dos dados lidos; nada é enviado a ninguém).
- Cada comando é de leitura, sem `Elevate`. Onde há ganho com administrador, o campo extra aparece e, sem admin, vira `Skipped.NeedsAdmin` (nunca eleva sozinho).

### Itens, comandos de leitura e achados

Todos os formatos de saída abaixo são hipóteses de trabalho: o PR começa com fixtures sintéticas e a conferência real fica para o smoke de VM (**A VERIFICAR** para cada formato). Saídas dependem de idioma: forçar `LC_ALL=C` no Unix; no Windows preferir PowerShell com saída em JSON (`ConvertTo-Json`) a `netsh` com texto localizado.

| # | Item | Windows | Linux | macOS | Admin ganha |
|---|---|---|---|---|---|
| 1 | IPv4/IPv6 em uso, gateway | `Get-NetIPConfiguration` / `Get-NetIPAddress` (JSON) | `ip -j addr`, `ip -j route` | `networksetup -listallhardwareports`, `ifconfig`, `netstat -rn` | nada |
| 2 | Perfil de rede (pública/privada/domínio) | `Get-NetConnectionProfile` (`NetworkCategory`) | não existe equivalente (NetworkManager: `nmcli`, zona do firewalld) | não existe equivalente | nada |
| 3 | Tipo de segurança do Wi-Fi | `netsh wlan show interfaces` (campo de autenticação e cifra da conexão atual; localizado) | `nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no` ou `iw dev <if> link` | `system_profiler SPAirPortDataType` ou `wdutil info` (esta exige `sudo`) | macOS: `wdutil` |
| 4 | DNS (resolvedores, DoH/DoT) | `Get-DnsClientServerAddress`; DoH: `Get-DnsClientDohServerAddress` (existência por versão: A VERIFICAR) | `resolvectl status` ou `/etc/resolv.conf` | `scutil --dns` | nada |
| 5 | Portas em escuta | `Get-NetTCPConnection -State Listen`, `Get-NetUDPEndpoint`, processo via `OwningProcess` | `ss -H -ltnup` | `lsof -nP -iTCP -sTCP:LISTEN` | nome do processo de outros usuários em `ss`/`lsof` |
| 6 | Firewall ligado | `Get-NetFirewallProfile` (`Enabled`, ação padrão de entrada) [7] | `ufw status`, `firewall-cmd --state`, `nft list ruleset` (`ufw`/`nft` pedem root; sem root: "não dá para saber") | `/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate`; `pf`: `pfctl -s info` (pede root) | Linux e macOS pf |
| 7 | IPv6 privacy extensions | `Get-NetIPv6Protocol` (`UseTemporaryAddresses`, **A VERIFICAR**) | `sysctl net.ipv6.conf.all.use_tempaddr` | `sysctl net.inet6.ip6.use_tempaddr` | nada |
| 8 | Portal cativo | ver abaixo (requisição externa, **sob pedido**) | idem | idem | nada |

(`ip -j`, `resolvectl`, `nmcli`, `iw`, `networksetup`, `scutil`, `socketfilterfw`: comandos conhecidos do ecossistema; a disponibilidade e as opções por versão são **A VERIFICAR**. Nenhum foi executado para este plano. `airport` é um utilitário antigo e removido em versões recentes do macOS (**A VERIFICAR**), por isso não é a primeira escolha.)

**Nenhum comando de leitura de Wi-Fi pode disparar varredura ativa (scan).** Varredura emite sinal de rádio, pode atrapalhar a conexão e, em rede monitorada, parece sondagem; esta função é leitura passiva do que o SO já sabe.

- **Linux:** a documentação do NetworkManager diz que `nmcli device wifi list`, **por padrão, garante que a lista de pontos de acesso tenha no máximo 30 segundos e dispara uma varredura se for preciso**, e que `--rescan` aceita `auto`, `no` e `yes`, podendo forçar ou desativar a varredura [14]. Logo o toolkit usa **sempre** `nmcli ... dev wifi list --rescan no` (lista em cache, sem varredura; observação: o cache pode estar velho ou vazio, e o item mostra "não verificado" nesse caso). Um `nmcli ... dev wifi` sem `--rescan no` é **proibido** (teste de regressão: o `Cmd` do Wi-Fi sempre leva `--rescan no`). `iw dev <if> link` e `iw dev <if> info` só leem o estado da conexão atual; **`iw dev <if> scan` é proibido** (faz varredura; exigiria root).
- **Windows:** `netsh wlan show interfaces` lê a conexão atual e **não lista redes vizinhas**; é o comando do plano. `netsh wlan show networks` mostra "todas as redes visíveis ao alcance" [15], mas a página não diz se usa o cache do serviço WLAN ou se força varredura: **A VERIFICAR** na VM; até lá o toolkit **não o usa**. `netsh wlan show profiles`/`export ... key=clear` não são usados (senha, ver abaixo).
- **macOS:** `system_profiler SPAirPortDataType` e `wdutil info` mostram a conexão atual e, em versões que listam redes próximas, o `system_profiler` pode incluir vizinhas: se isso envolve varredura ativa é **A VERIFICAR**; o toolkit lê só o bloco da **interface conectada** e descarta o resto. `airport -s` (varredura) é proibido.
- No teste, o runner falso falha qualquer `Cmd` de Wi-Fi que não esteja numa lista fechada de comandos sem varredura.

**Portal cativo.** Detectar exige uma requisição a um endereço de teste, igual ao IP público: entra na mesma regra "só sob pedido", com o mesmo serviço/escolha. Sem pedido, o item mostra "não verificado".

### Achados e severidades

| Achado | Severidade | O que fazer (texto na tela) |
|---|---|---|
| Rede Wi-Fi **aberta** (sem senha) | alta | evite dados sensíveis; use VPN; prefira outra rede |
| Wi-Fi **WEP** | alta | WEP é quebrado; troque o roteador para WPA2 ou WPA3 |
| Wi-Fi WPA (TKIP) ou WPA2 misto | média | prefira WPA2-AES ou WPA3 |
| Wi-Fi WPA2-AES | informativa | adequado; WPA3 é melhor se o roteador e as placas aceitarem |
| Wi-Fi WPA3 | OK | |
| Firewall **desligado** num perfil ativo | alta | ligue o firewall (o toolkit não liga; mostra o comando/caminho manual) |
| Perfil de rede **Pública** com compartilhamento ligado, ou **Privada** em rede pública | média | ajustar o perfil da rede em Configurações |
| Porta sensível **escutando em endereço não local** (lista fechada no código: 21, 22, 23, 135, 139, 445, 3389, 5900-5901, 5985/5986, 3306, 5432, 6379, 27017, 9200 e similares; lista final e severidade por porta: a decidir no PR) | alta ou média conforme a porta e o firewall | feche o serviço ou restrinja ao `127.0.0.1`; o toolkit não fecha porta |
| Porta qualquer em `0.0.0.0` | informativa | explicar o que é; sem alarmar |
| DNS em IP público desconhecido sem DoH/DoT | baixa | sem ação obrigatória; considere DNS com criptografia |
| IPv6 sem privacy extensions | baixa | endereço estável rastreável; ativar privacy extensions (instrução manual) |
| Dois ou mais resolvedores DNS inalcançáveis, gateway ausente | média | rede sem conexão; checar cabo/roteador |

A lista de portas e a severidade ficam em **tabela fechada no código**, revisada no PR. Nenhuma dessas leituras identifica "ataque": a função não afirma "você foi invadido", só "isto está exposto".

### O que NUNCA fazer

- Varrer portas, hosts ou a sub-rede de outros (nem a do próprio roteador); nada de `nmap`, ping sweep, ARP scan ou sondagem.
- Alterar rede, firewall, DNS, perfil ou Wi-Fi (esta função é só leitura; mudar é outra função, outro PR).
- Fazer requisição externa sem pedido explícito, ou enviar qualquer dado da máquina.
- Gravar senha de Wi-Fi (`netsh wlan show profile key=clear`, `nmcli ... show-secrets`, Keychain): **proibido**; o toolkit só lê o **tipo** de segurança.
- Executar com `Elevate`.

### TUI (área 4 Rede, seção "Análise")

Estados: Carregando (`| Lendo a rede…`), Resultado (lista de itens com o selo ok/atenção/problema e o "o que fazer", painel de detalhe com `d` como no Diagnóstico), Sem permissão em partes (`Skipped.NeedsAdmin` explícito, "rode como administrador para ler X"), e a ação opcional "Descobrir meu IP público" com diálogo de confirmação que **nomeia o serviço**, foco em Cancelar. Início: o item "Analisar a rede" deixa de ser "em breve".

### Testes

Parse de cada saída (fixtures sintéticas, em pt-BR e en-US para o Windows), regras de achado por tabela (cada limiar com teste de fronteira), porta sensível só em endereço não local, firewall indeterminado sem root não vira "ligado", nenhum comando sem `ReadOnly`, nenhum `Elevate`, rede indisponível, texto de SSID malicioso, IP público: só com pedido e só com o serviço configurado (fake HTTP), hostname/SSID mascarados no log de execução. Contrato: teste que lista **todos** os comandos que o pacote pode emitir e confere que nenhum é de escrita.

---

## Função 3: otimização do Windows

**Escopo.** Duas partes: (A) **leitura** de gargalos, com achados e "o que fazer"; (B) a **única mudança**: reduzir animações, com plano, backup e desfazer. TUI: área 2 Máquina, seção "Otimização". Pacote novo `internal/otimizacao` (leitura e mudança separadas), só Windows; fora do Windows os comandos terminam com "só existe no Windows" (como a privacidade).

### A) Leitura de gargalos (sem administrador, sem mudar nada)

| Área | Fonte (PowerShell de texto fixo, JSON) | Achado |
|---|---|---|
| CPU | `Get-Counter '\Processor(_Total)\% Processor Time'` ou `Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor` (amostra curta) | uso alto sustentado; processos que mais consomem (`Get-Process`, só nome/CPU/memória) |
| RAM | `Get-CimInstance Win32_OperatingSystem` (livre/total) | pouca memória livre, muito uso de arquivo de paginação |
| Disco | `Get-PhysicalDisk` (tipo SSD/HDD, saúde), contadores de fila/uso do disco | HDD como disco do sistema; fila alta; pouco espaço livre |
| Inicialização | `Get-CimInstance Win32_StartupCommand` e chaves `Run`/`RunOnce` (só leitura) e pastas de inicialização | muitos itens de inicialização; lista com nome e fabricante |
| Serviços | `Get-CimInstance Win32_Service` com `StartMode='Auto'` e estado | quantidade de serviços automáticos de terceiros; serviço automático parado (falhou) |
| Tempo de boot | log de eventos `Microsoft-Windows-Diagnostics-Performance/Operational`, evento **100** (boot) [8] | duração do último boot e a flag de degradação |

Notas de honestidade sobre o log de boot: o evento 100 é descrito como o aviso de que o Windows iniciou **e uma lentidão foi detectada** [8]; relatos em fóruns dizem que ele pode **não existir** em algumas builds do Windows 10 ou o log operacional vir vazio [8]. O toolkit trata "sem evento" como "não medido", nunca como "boot rápido". O mapeamento de 101 e 102 (aplicativo e driver, segundo relato comunitário) e o evento de **desligamento (200)** citado no pedido **não foram confirmados** em fonte oficial: **A VERIFICAR**. Leitura do log operacional pode exigir administrador (**A VERIFICAR**); sem admin, `Skipped.NeedsAdmin`.

Achados (todos com "o que fazer" manual, nenhuma ação automática):

| Achado | Severidade | O que fazer |
|---|---|---|
| Disco do sistema é HDD | média | trocar por SSD melhora mais que qualquer ajuste |
| RAM livre abaixo de limiar (a definir no PR com fronteira testada) | média | fechar programas ou ampliar a memória |
| Muitos itens de inicialização | baixa | abrir Configurações > Aplicativos > Inicialização e desligar os que não usa (o toolkit não desliga) |
| Boot com degradação (evento 100) | baixa | ver o que atrasa (itens de inicialização, drivers) |
| Pouco espaço livre no disco do sistema | média | liberar espaço |
| Serviço automático parado | baixa | informativo |

Nada de ranking de "pontuação" mágica; só números, limiares explícitos e documentados (padrão do `docs/diagnostico.md`).

### B) Animações: plano, backup e desfazer

**Mecanismos conhecidos.** A API do Windows documenta chaves "interruptor" via `SystemParametersInfo`: `SPI_SETCLIENTAREAANIMATION` (animações na área cliente), `SPI_SETANIMATION` (animação de janelas, estrutura `ANIMATIONINFO`, é o `MinAnimate`), `SPI_SETMENUANIMATION` (animação de menus, "interruptor mestre"), `SPI_SETCOMBOBOXANIMATION`, `SPI_SETLISTBOXSMOOTHSCROLLING`, `SPI_SETTOOLTIPANIMATION` e `SPI_SETUIEFFECTS` (todos os efeitos de UI de uma vez) [9]. Os `SPIF_UPDATEINIFILE` e `SPIF_SENDCHANGE` gravam no perfil do usuário e avisam os programas [9]. São **configurações por usuário** (`HKCU`).

O que o doc da API **não** diz: os nomes de valor de registro que correspondem a cada uma (a documentação lida não os cita [9]). O pedido cita `VisualFXSetting`, `MinAnimate` e `TaskbarAnimations`: os caminhos e valores abaixo são o que se sabe do uso comum do Windows e **precisam ser confirmados em VM antes do código**:

| Chave provável (HKCU) | Valor | Controla | Status |
|---|---|---|---|
| `Control Panel\Desktop\WindowMetrics` | `MinAnimate` (texto "0"/"1") | animar janelas ao minimizar/maximizar | A VERIFICAR |
| `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced` | `TaskbarAnimations` (DWORD) | animações da barra de tarefas | A VERIFICAR |
| `Software\Microsoft\Windows\CurrentVersion\Explorer\VisualEffects` | `VisualFXSetting` (DWORD) | perfil geral "melhor desempenho/aparência/personalizado" | A VERIFICAR |
| `Control Panel\Desktop` | `UserPreferencesMask` (binário) | máscara de vários efeitos | **não mexer direto**: é uma máscara binária frágil |

**Regra de implementação recomendada:** **usar `SystemParametersInfo` (a API documentada [9])** em vez de editar `UserPreferencesMask` na mão, e só gravar o valor de registro quando a API não cobre (TaskbarAnimations). Mudar por API faz o efeito aparecer sem sair da sessão; a API de leitura (`SPI_GET...`) é o que alimenta o backup do valor original. A chamada de API é feita por um **PowerShell de texto fixo** ou por `x/sys/windows` (decisão de implementação no PR; sem dependência nova sem justificar). Quais valores são seguros e reversíveis: todos os da tabela de `SPI_SET...` acima são interruptores booleanos reversíveis por definição da API [9]; confirmar na VM que a reversão devolve o mesmo estado visual.

- **Escopo:** por usuário (`HKCU`/API do usuário). **Nenhuma** parte exige administrador, e **nada** muda em `HKLM`. Isso facilita: o administrador não é necessário, e a regra de nunca elevar fica trivial.
- **Plano:** lista de itens com "valor atual -> novo" e o efeito esperado em linguagem simples ("janelas minimizam sem animação").
- **Backup:** valor original de cada item (da API de leitura ou do registro, incluindo "não existia"), em `otimizacao-backup-<SID>.json` (0600, atômico, conferido contra o catálogo, padrão da privacidade).
- **Desfazer:** `toolkit otimizacao desfazer` restaura cada item, apaga o backup. Aplicar duas vezes não sobrescreve o original.
- **Efeito:** imediato por `SPIF_SENDCHANGE` [9]; algumas animações do Explorer podem só valer após reiniciar o Explorer ou sair e entrar na sessão (**A VERIFICAR**; a mensagem não promete o que não testou).
- Acessibilidade: reduzir animação é um recurso de acessibilidade do próprio Windows; o toolkit só liga o que o usuário aceitar no plano.

### O que NÃO fazer (lista fixa, em teste de regressão)

- Desativar serviço de segurança (Defender, Firewall, Atualização do Windows, Centro de Segurança), nem qualquer serviço (nesta função **nenhum serviço é alterado**).
- "Otimizadores" agressivos: limpar registro, desativar o arquivo de paginação, desativar o SmartScreen/UAC, mexer em `HKLM`, desligar telemetria (isso é a função de privacidade).
- Remover componentes do sistema ou do Windows Update; apagar arquivos do sistema; `dism`.
- Matar processo, desabilitar item de inicialização ou tarefa agendada (só mostra; o usuário decide).
- Qualquer "pontuação de PC" ou promessa de ganho de desempenho: animação reduzida melhora a **sensação**, não prova ganho; a tela diz isso.

### TUI (área 2 Máquina, seção "Otimização")

Duas partes na mesma seção: "Gargalos" (leitura, resultado com selos e detalhe) e "Animações" (plano, confirmação com foco em Cancelar, faixa de resultado, Desfazer). Estados padronizados; fora do Windows, o estado "só existe no Windows".

### Testes

Parse de cada leitura (fixtures sintéticas), limiares com fronteira, `SPI` e registro via fake (`FakeSystem` como o da privacidade): aplicar/desfazer ida e volta, valor "não existia", duas aplicações, backup adulterado recusado, nenhuma escrita em `HKLM`, nenhum serviço tocado (teste que falha se o catálogo contiver serviço), nenhum `Elevate`, saída sem tela com `--yes`/`--dry-run`.

---

## Função 4: mais instalações no catálogo

**Estado atual** (`catalog/catalog.yaml`): git, vscode, python, discord, firefox, vlc, 7zip. O catálogo é **fechado e revisado por PR**; formato em [catalogo.md](catalogo.md). Regras: nada de download sem SHA-256, nada de `curl | bash` de terceiros, nada que contorne licenciamento. Nenhum comando de pacote foi executado para este documento; ids vêm de páginas públicas (repositório `microsoft/winget-pkgs`, API do Homebrew, Flathub), consultadas em 2026-11-14.

### Navegadores

| App | Licença | winget | brew | flatpak | apt/dnf | Recomendação |
|---|---|---|---|---|---|---|
| Firefox | MPL | `Mozilla.Firefox` (já no catálogo) | cask `firefox` | `org.mozilla.firefox` | já no catálogo | já existe |
| Brave | código aberto (MPL-2.0; **A VERIFICAR**) | `Brave.Brave` (existe no repositório) | cask `brave-browser` (existe) | `com.brave.Browser` (existe na Flathub) | apt/dnf exigem repositório de terceiros: **fora** | **entra** (windows, macos, linux por flatpak) |
| Google Chrome | proprietário (gratuito) | `Google.Chrome` (existe) | cask `google-chrome` (existe) | `com.google.Chrome` (existe; licença marcada como proprietária na Flathub) | repositório de terceiros: fora | **entra** com a ressalva abaixo |
| Microsoft Edge | proprietário | já vem no Windows | cask: a confirmar | a confirmar | a confirmar | **não entra** (já vem no Windows) |

Ressalva Chrome: o instalador do winget aponta para uma URL do fabricante que muda de conteúdo a cada versão, e o mesmo ocorre em outros gerenciadores; hash divergente (`ErrHashMismatch`, sem retry) é o comportamento **correto** do toolkit (nunca `--ignore-security-hash`), mas pode aparecer como "falhou" até o manifesto ser atualizado. A tela explica. Chrome é de uma empresa que coleta telemetria: o catálogo mostra a categoria e o nome, sem juízo de valor.

### Escritório e compactadores

| App | Licença | winget | brew | flatpak | Linux nativo | Recomendação |
|---|---|---|---|---|---|---|
| LibreOffice | MPL-2.0 (**A VERIFICAR**) | `TheDocumentFoundation.LibreOffice` (existe; também há variantes `PreRelease`, `HelpPack`, `SDK`: **usar só a estável**) | cask `libreoffice` | `org.libreoffice.LibreOffice` | apt/dnf `libreoffice` (a confirmar) | **entra** |
| 7-Zip | LGPL e BSD (a fórmula do Homebrew informa `LGPL-2.1-or-later AND BSD-3-Clause`) | `7zip.7zip` (já no catálogo) | fórmula `sevenzip` (já no catálogo) | | `p7zip-full`/`p7zip` (já) | já existe |
| PeaZip | LGPL-3.0 (**A VERIFICAR**) | `Giorgiotani.Peazip` (existe) | cask: a confirmar (a consulta à API do Homebrew não retornou) | `io.github.peazip.PeaZip` (existe na Flathub) | a confirmar | **entra** (alternativa ao 7-Zip com interface gráfica) |
| GNOME File Roller | GPL | n/a | n/a | `org.gnome.FileRoller` (existe) | | **não entra**: já vem em muitas distribuições |
| Keka (macOS) | a confirmar | n/a | cask `keka` (existe) | | | **não entra agora**: o macOS abre ZIP sozinho; 7zip do brew já cobre o resto |

### Acesso remoto gratuito e vitalício (avaliação)

"Gratuito e vitalício" nenhum software de empresa **garante**: os termos mudam. A única garantia real é **código aberto com licença livre**, que permite hospedar o próprio servidor e continuar usando a versão que existe.

| Opção | Licença e custo | Segurança (o que se sabe) | Decisão |
|---|---|---|---|
| **RustDesk** | cliente e servidor em **AGPL-3.0** (confirmado na API do GitHub e na Flathub [10]); existe plano pago para servidor "Pro" | modelo de relay: pode usar os servidores públicos do projeto ou **servidor próprio** (hbbs/hbbr). Houve **muitos relatos de CVE em 2026** (agregadores citam um lote de 6 em março, falhas no servidor e no cliente, incluindo desvio de autorização e reprodução de sessão); versões e correções divergem entre as fontes: **A VERIFICAR no NVD e nos avisos do GitHub antes de recomendar uma versão mínima** [11]. É também abusado em golpes de falso suporte (conhecimento público; **A VERIFICAR** com fonte). Criptografia e 2FA: **A VERIFICAR** na documentação do projeto | **entra com ressalvas fortes** (ver abaixo) |
| TightVNC | **Duas linhas de licença diferentes** [12]: o **TightVNC 2.x é GPL** (livre, inclusive uso comercial, cópias modificadas também GPL); o **TightVNC 4.x é software fechado/proprietário**, com versão base gratuita para uso pessoal e comercial. A **criptografia de sessão (TLS 1.3) é só na assinatura paga Pro** [12]. Qual das duas linhas o id `GlavSoft.TightVNC` do winget instala: **A VERIFICAR** | VNC puro: sem criptografia de sessão na versão livre; exposição de porta 5900; não usar pela internet sem túnel (SSH/VPN). Histórico de CVEs: **A VERIFICAR** | **não entra** no catálogo padrão |
| UltraVNC / TigerVNC | GPL | mesmo desenho do VNC; TigerVNC existe no winget (`TigerVNC.TigerVNC`) e na Flathub (`org.tigervnc.vncviewer`, só cliente); recursos de criptografia por versão: **A VERIFICAR** | **não entram** agora (mesma razão do TightVNC; reavaliar com túnel documentado) |
| Chrome Remote Desktop | proprietário gratuito do Google; depende de conta Google | detalhes de criptografia e permanência do serviço: **A VERIFICAR** | **não entra** (depende de conta e de serviço de terceiro) |
| AnyDesk, TeamViewer | proprietários; gratuitos só para uso pessoal, termos mudam; AnyDesk teve incidente público de segurança (**A VERIFICAR**) | proprietários, sem auditoria aberta | **não entram** |
| Assistência Rápida (Quick Assist) | já vem no Windows | exige conta Microsoft para o ajudante (**A VERIFICAR**) | não é app a instalar; só citar na ajuda |

**Recomendação para o RustDesk (decisão do Paulo, ver [decisões](#decisões-abertas)):**

1. Só entra se o Paulo aceitar manter uma **versão mínima** e revisar a cada CVE; se entrar, entra com o pacote do gerenciador do SO, **nunca** com um instalador baixado fora do gerenciador sem SHA-256.
2. **Windows:** o id `RustDesk.RustDesk` **não está no repositório winget-pkgs** (a listagem do diretório do fabricante não retornou): fica "a confirmar" e, enquanto isso, a opção é o provider `github-release` com o `sha256` **fixado no catálogo** (hoje o catálogo fixo cobre um hash por release, ou seja, um PR de atualização por versão). **macOS:** cask `rustdesk` (existe, versão 1.5.3 e hash na API do Homebrew em 2026-11-14). **Linux:** flatpak `com.rustdesk.RustDesk` (existe; AGPL-3.0-only na Flathub).
3. O catálogo não configura o RustDesk: não troca servidor, não ativa autoinício. A ajuda diz: use senha forte e senha temporária, desative "aceitar acesso permanente" se não precisa, prefira servidor próprio, e **só instale se a pessoa que vai usar entende que isto dá controle da máquina**.
4. Se a decisão for não aceitar o risco de CVEs, a alternativa recomendada é **nada de acesso remoto no catálogo** (Assistência Rápida no Windows e Compartilhamento de Tela no macOS são nativos).

### Verificação do PR13 (RustDesk e versão mínima)

Pesquisa feita em 2026-11-14 na API pública do NVD ([consulta por palavra-chave `rustdesk`](https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=rustdesk&resultsPerPage=100), 21 CVEs), nos
[releases do GitHub](https://github.com/rustdesk/rustdesk/releases) e na [Flathub](https://flathub.org/apps/com.rustdesk.RustDesk). CVEs de severidade alta ou crítica no **cliente**:

| CVE | Severidade (NVD) | Afetado segundo o NVD | Corrigido em |
|---|---|---|---|
| [CVE-2026-30791](https://nvd.nist.gov/vuln/detail/CVE-2026-30791), [30785](https://nvd.nist.gov/vuln/detail/CVE-2026-30785), [30793](https://nvd.nist.gov/vuln/detail/CVE-2026-30793), [30795](https://nvd.nist.gov/vuln/detail/CVE-2026-30795), [30797](https://nvd.nist.gov/vuln/detail/CVE-2026-30797) | alta e crítica (8.2 a 9.3) | até 1.4.5 | 1.4.6 (lançada em 2026-03-05) ou depois: **inferido** (o NVD só diz "até 1.4.5") |
| [CVE-2026-30792](https://nvd.nist.gov/vuln/detail/CVE-2026-30792), [30798](https://nvd.nist.gov/vuln/detail/CVE-2026-30798) | alta (8.3 e 8.2) | **divergente no próprio NVD**: texto "até 1.4.8", lista de produtos "até 1.4.5" | tratada como 1.4.9 ou depois (leitura conservadora do texto) |
| [CVE-2026-57850](https://nvd.nist.gov/vuln/detail/CVE-2026-57850) e [58056](https://nvd.nist.gov/vuln/detail/CVE-2026-58056) | alta (8.7 e 7.2) | antes de 1.4.9 (57850); 58056: o NVD declara a **1.4.9 como não afetada** (no campo de produtos da própria entrada; fonte apontada na revisão da Lupa, reconferir no NVD) | 1.4.9 (a 57850 cita a [release 1.4.9](https://github.com/rustdesk/rustdesk/releases/tag/1.4.9)); a 58056 também fica corrigida na 1.4.9 segundo essa entrada do NVD |
| [CVE-2026-73108](https://nvd.nist.gov/vuln/detail/CVE-2026-73108) | alta (8.7, negação de serviço antes de autenticar) | antes de 1.4.7 | 1.4.7 |
| [CVE-2026-76840](https://nvd.nist.gov/vuln/detail/CVE-2026-76840) | alta (8.5, só Windows, área de transferência) | o NVD ainda sem versão (análise adiada), mas o registro CVE (cvelistV5, do projeto CVE) a declara **afetada até a 1.4.9** | o NVD aponta o PR 15515 ("harden wf_cliprdr.c"), que consta nas notas da **1.5.0**: a 1.5.0 contém a correção (confirmado contra as notas da release; versão exata de correção continua **inferida** por não constar no NVD) |

Outros, de severidade média ou baixa e já com versão (`CVE-2026-100388` e `CVE-2026-100417`: antes de 1.5.0; `CVE-2026-73102`, macOS: 1.3.9 a 1.4.9) também ficam atrás da 1.5.0. Fora de escopo: `CVE-2026-3598` (servidor Pro) e `CVE-2026-111324` (rustdesk-api), do servidor.
`CVE-2024-25140` (certificado de teste instalado no Windows, 1.2.3) tem a posição do fabricante no próprio NVD (comportamento intencional do instalador).

**Versão mínima adotada: 1.5.0** (lançada em 2026-09-30; a atual na verificação era a 1.5.3, de 2026-11-09, igual à do cask `rustdesk` e à da Flathub). É a menor versão
**sem CVE alto ou crítico conhecido sem correção** pelos dados acima; o ponto mais frágil é o que está marcado como inferido ou A VERIFICAR (o NVD ainda tem registros "adiados"
e com informação divergente: reconferir antes de cada PR que mexa na entrada). Fonte oficial adicional não consultada: os avisos de segurança do GitHub do projeto (a API devolveu lista vazia).

**O toolkit enforça a versão mínima?** Não. O catálogo instala "a versão atual" do gerenciador: o brew e o flatpak **recusam** fixar versão (`SupportsVersion` sempre recusa) e o
`versions: true` não vale para eles. Hoje, brew e Flathub entregam a 1.5.3 (acima da mínima), mas um espelho ou cache atrasado poderia entregar menos, e o toolkit não confere a versão
depois de instalar. Por isso o aviso na tela diz a versão mínima, a data e a fonte e manda o usuário conferir a versão no app, e **nunca afirma** que a instalada cumpre.
Aceitável para a decisão 3 porque o ganho é só para quem aceita o aviso e o risco residual é o de uma instalação desatualizada, que o próprio aviso descreve.
**Pendência (PR de enforcement, depois do smoke):** conferir a versão instalada depois do install (ou no plano, via `brew info`/`flatpak remote-info`) contra um campo `min_version` do catálogo e falhar/avisar alto se for menor.

**Windows:** o id `RustDesk.RustDesk` segue **sem pasta** em `microsoft/winget-pkgs` (`manifests/r/RustDesk` devolve 404 na API do GitHub em 2026-11-14), então o Windows fica sem instalador (o plano diz isso, nada é inventado).
Outro id só se o próximo PR o verificar em fonte pública.

**Aviso (`note:`)**: o campo opcional novo do catálogo ([catalogo.md](catalogo.md#aviso-note)) leva o texto da recomendação 3 acima. Aparece no plano do `install`/`--dry-run` (antes de confirmar), na Revisão da TUI (lista de avisos) e no detalhe da aba Apps.

### Como entra no catálogo

Cada app vira uma entrada de `catalog/catalog.yaml` com o formato de [catalogo.md](catalogo.md): `name`, `category` (`navegadores`, `escritorio`, `utilitarios`; acesso remoto: `utilitarios` ou uma categoria nova, que seria uma linha na lista do código), instaladores por SO e o bloco `detect` **só com valores conhecidos** (sem certeza, sem o bloco). `assets_test.go` passa a conferir o número de entradas. ids por gerenciador: só os da tabela acima marcados como existentes; o resto fica **fora** até o PR confirmar. Um PR por grupo (navegadores; escritório e compactadores; acesso remoto, separado por ter decisão própria).

### TUI e ajuda

A aba **Apps** já existe e lê o catálogo: **sem mudança de UI**. Impacto: o Início e a ajuda (`?`) precisam citar o novo conteúdo (o objetivo "Instalar programas" sugere "navegadores, escritório, compactadores"); o texto de acesso remoto, se entrar, vem com o aviso de segurança acima na tela de detalhe do app (descrição curta no catálogo, sem link de marketing). Contagens em `docs/tui.md`, README e testes de golden da lista são atualizadas no mesmo PR.

### Testes

`assets_test.go` (entradas, toda `url` com sha256), validação de catálogo (id, categoria, provider certo por campo, `detect` válido), plano `--dry-run` por app por SO (fake) e teste de que nenhum app novo tem `url` sem `sha256` ou provider de terceiro fora da lista.

---

## Riscos transversais e segurança do plano

| Função | Risco | Gravidade | Mitigação | Teste obrigatório |
|---|---|---|---|---|
| USB | **Trancar o usuário fora** (teclado/mouse bloqueado) | alta | recusar se teclado/mouse conectado ficar fora; timer de reversão; backup; roteiro de recuperação na tela; teclado reserva | recusa por teclado fora; timer expira; ida e volta |
| USB | `DenyUnspecified` bloqueia **todos** os dispositivos novos, não só USB (rede, vídeo, impressora) | alta | camada (`AllowDenyLayered`) em vez de `DenyUnspecified`; negação limitada ao barramento USB (por ID/classes de USB), **nunca** por classe `Keyboard`/`Mouse`/`HIDClass`/`DiskDrive`; prova de essenciais pelo PnP; mostrar no plano | plano nunca contém classe fora da lista fechada; testes de teclado I2C/PS2/Bluetooth e disco NVMe/SATA |
| USB | **Falso senso de segurança** (clonagem de VID/PID; dispositivo já instalado continua) | média | aviso fixo "reduz, não elimina" na tela, no CLI e na doc; não usar a palavra "proteção total" | teste de texto presente em todos os fluxos |
| USB | ID compatível genérico na lista libera toda a classe | alta | só IDs de hardware com VID e PID | teste recusa ID genérico |
| USB | Tarefa de reversão vira **escalonamento de privilégio** (binário editável) | alta | comando fixo, ACL da pasta, preferir script embutido; mostrar no plano | teste do comando exato e de ACL na pasta |
| USB (Linux) | `usbguard` bloqueia teclado na sessão | alta | `start` agora, `enable` só após confirmar; recusa se teclado fora das regras; `systemd-run` de reversão | idem |
| Windows (geral) | **Quebrar o Windows** (mudar `HKLM`, serviço) | alta | otimização só em `HKCU`; USB só em `Restrictions`; catálogos fechados; nada de serviço | teste "nenhuma escrita fora do catálogo" |
| Rede | **Privacidade dos dados lidos** (IP, MAC, SSID, portas) | média | só local; aviso antes de `--relatorio`/`--json`; nada sai da máquina; IP público só sob pedido com o serviço nomeado | fake HTTP: nenhuma requisição sem pedido |
| Rede | requisição externa revela o IP a um terceiro | média | só sob pedido; serviço fixo ou configurável; sem cookie; resposta validada | idem |
| Rede | **Abuso do toolkit** para varredura de terceiros | média | nenhum comando aceita alvo digitado; só endereços da própria máquina | teste: nenhum Cmd recebe host de entrada do usuário |
| Otimização | prometer ganho que não existe | baixa | texto honesto; sem pontuação | texto presente |
| Apps | pacote adulterado, instalador sem hash | alta | só provider de gerenciador ou `sha256` fixado; sem `curl` encadeado em shell | validação do catálogo |
| Apps | acesso remoto abusado em golpes | média | texto de aviso; decisão do Paulo | texto presente |
| Todas | **Backup adulterado** vira escrita arbitrária | alta | conferir cada entrada contra o catálogo antes de restaurar | teste de backup adulterado |
| Todas | Texto do sistema com ESC/bidi redesenha o terminal | média | `textsafe` em toda saída lida | testes de injeção |

### Desfazível e não desfazível

| Função | Desfazível | Não desfazível |
|---|---|---|
| USB (Windows e Linux) | tudo, enquanto o backup existir (e por timer até confirmar) | dispositivo que **já foi bloqueado retroativamente** e foi desconectado nesse intervalo precisa ser reconectado (nada se perde); pacote instalado pelo toolkit só sai se o usuário pedir |
| Rede | nada muda | n/a |
| Otimização (animações) | tudo | n/a (leitura não muda) |
| Apps | desinstalar fica fora do toolkit (decisão do projeto: instalar, não desinstalar) | tudo que o instalador do app faz |

### Administrador e elevação explícita

| Função | Precisa de administrador | Como é explícito |
|---|---|---|
| USB, Windows | sim (escreve `HKLM`, cria tarefa do sistema) | recusa antes de qualquer mudança; mensagem manda abrir o terminal como administrador |
| USB, Linux | sim (`sudo -n`) | `ErrNeedsAdmin` e dica `sudo -v`; o plano mostra o `sudo -n -- ...` como será executado |
| USB, macOS | não (só orientação) | n/a |
| Rede | não (leitura); mais campos com admin | `Skipped.NeedsAdmin`, nunca eleva sozinho |
| Otimização | não | n/a |
| Apps | como já é (apt/dnf com `sudo -n`; winget pede o UAC) | plano e `[s/N]` já existentes |

### `security-audit` em modo guia (por PR)

Arquivos de referência da skill a consultar, só os que tocam o PR (sem criar pasta nem relatório; linha "Segurança" no relatório do PR):

| PR | Referências |
|---|---|
| Rede | `WEB-PROTOCOL-AND-AUTH.md` (requisição HTTP opcional do IP público: TLS, redirect, SSRF, limites); injeção de saída (`textsafe`); privacidade |
| Apps | `SUPPLY-CHAIN-AND-RELEASE.md` (hash, origem, versões fixas, provider `github-release`) |
| Otimização | injeção em comandos PowerShell de texto fixo; backup como entrada não confiável; privilégio mínimo (`HKCU`) |
| USB | `CLOUD-AND-DEPLOYMENT.md` não se aplica; valem execução de comando privilegiado, tarefa agendada como vetor de escalonamento, ACL de arquivo de estado, backup não confiável, falha segura (recusar na dúvida) |

Regras da skill respeitadas: nada de sondar produção, nada de gastar cota de API paga, código do alvo só em sandbox/VM.

---

## Ordem dos PRs

Continuação da numeração dos PRs de produto (o último documentado é o PR8 da fase 2; estes são os próximos de segurança/TUI, numerados aqui como proposta). Cada PR é pequeno, com TUI, testes, docs (este documento e o `CLAUDE.md`), `scripts/ci.sh` verde, `security-audit` em modo guia e revisão de PR.

| PR | Escopo | Depende de | Risco | Critério de pronto |
|---|---|---|---|---|
| **PR8** Rede: análise (só leitura) (feito: `docs/rede.md`) | `internal/rede` (Windows, Linux, macOS), achados e severidades, `toolkit rede` (`--json`, `--relatorio`), TUI área 4 > Análise, Início "Analisar a rede"; **sem IP público** | nada | baixo (leitura) | todos os itens da tabela com fixtures; nenhum `Elevate`; nenhum comando de escrita; golden TUI; `docs/rede.md` |
| **PR9** Rede: IP público e portal cativo (sob pedido) | requisição externa com serviço nomeado, confirmação com foco em Cancelar | PR8 e decisão sobre o serviço | médio (requisição externa) | fake HTTP prova "nada sem pedido"; validação de resposta; texto de privacidade |
| **PR10** Apps: navegadores + escritório + compactadores | entradas no `catalog.yaml` (Brave, Chrome, LibreOffice, PeaZip) só com ids confirmados; testes e docs | decisão de lista | baixo | `assets_test.go`; `--dry-run` por app; ids confirmados; ajuda atualizada |
| **PR11** Otimização: leitura de gargalos | `internal/otimizacao` (leitura), `toolkit otimizacao`, TUI área 2 > Otimização (parte Gargalos) | nada | baixo | fixtures; limiares com fronteira; "sem evento" não vira "rápido" |
| **PR12** Otimização: animações (muda `HKCU`) | plano, backup, desfazer, API `SystemParametersInfo`, TUI parte Animações | PR11, valores confirmados em VM | médio | ida e volta; nenhuma escrita em `HKLM`; nenhum serviço |
| **PR13** Apps: acesso remoto (RustDesk, se aprovado) | entrada com aviso de segurança | decisão do Paulo e id confirmado | médio | aviso na tela; hash fixado; sem configuração do app |
| **PR14** USB: Windows | `internal/usb`, leitura PnP, plano, política em camadas, timer de reversão, backup/desfazer, TUI área 3 > USB permitidos, `docs/usb.md` com roteiro de recuperação | tudo acima, VM de laboratório | **alto** | todos os testes da função 1; roteiro de recuperação **testado em VM**; revisão reforçada; só depois do smoke do Paulo |
| **PR15** USB: Linux (USBGuard) | `usbguard` via apt/dnf, `rules.conf`, serviço com confirmação | PR14 (reuso do desenho) | alto | idem; sem pacote no gerenciador = "indisponível" |
| **PR16** USB: macOS (orientação) | texto e, se existir forma suportada, leitura; sem mudança | PR14 | baixo | texto honesto; nada executa |

Sugestão de **ordem**: PR8, PR10, PR11 podem andar em paralelo (independentes); PR9, PR12 e PR13 vêm depois deles; os PR14 a PR16 ficam por último por serem o maior risco. Cada fase para para revisão do Paulo (regra do projeto).

## Decisões abertas

| # | Pergunta | Recomendação |
|---|---|---|
| 1 | Qual serviço usar para descobrir o IP público (ou nenhum)? (PR8: aplicada a recomendação, nenhum serviço e nenhuma requisição; o PR9 trata) | **Nenhum por padrão**: só com serviço informado pela pessoa (`TOOLKIT_IP_URL`); decidir um padrão só depois de uma avaliação de privacidade do serviço escolhido |
| 2 | O portal cativo entra na primeira versão da análise de rede? (PR8: aplicada a recomendação, fora da fase) | **Não**: junto do IP público, no PR9 |
| 3 | RustDesk entra no catálogo, dadas as CVEs de 2026 e o uso em golpes? | **Resolvida no PR13 (2026-11-14): entra**, no macOS (cask) e no Linux (flatpak), com o aviso na tela e a versão mínima **1.5.0** verificada no NVD; **sem instalador no Windows** (id do winget inexistente). O toolkit NÃO enforça a versão mínima (ver [Verificação do PR13](#verificação-do-pr13-rustdesk-e-versão-mínima)) |
| 4 | Chrome entra apesar da possibilidade de falha de hash por URL do fabricante mutável? | **Entra**, com a mensagem explicativa; hash divergente nunca é contornado |
| 5 | USB no Windows Home: recusar ou tentar? | **Recusar** (a Microsoft não lista a edição); revalidar na VM |
| 6 | USB: identificar por ID de hardware (modelo) ou por ID de instância (unidade)? | **ID de hardware** com todos os nós do dispositivo; instância só se a VM mostrar que é estável |
| 7 | Prazo do timer de reversão | **10 minutos**, configurável por flag, mínimo 2 |
| 8 | Linux: aceitar USBGuard só onde há pacote no gerenciador? | **Sim**; Arch fica de fora até confirmar pacote oficial |
| 9 | macOS: só orientação? | **Sim** |
| 10 | Otimização: incluir mais que animações (ex.: efeitos de transparência, modo de energia)? | **Não** nesta rodada; só animações (reversíveis por usuário) |
| 11 | Categoria nova `remoto` para acesso remoto, ou `utilitarios`? | **`utilitarios`** (não criar categoria por um app) |
| 12 | Quem tem a VM de laboratório e quando roda o smoke de USB (Windows e Linux)? | Antes de iniciar o PR14; sem VM, o PR14 não começa |

## Pendências A VERIFICAR

1. Windows: o registro local de `DeviceInstall\Restrictions` funciona na edição Home (a doc do CSP lista só Pro, Enterprise, Education e IoT) [2].
2. Formato exato da subchave/lista de `AllowDeviceIDs` no registro (numerada, como o CSP mostra no valor `1&#xF000;...`) e todos os IDs de hardware dos nós de um teclado composto.
3. Efeito sem `gpupdate`/reinício; se a política vale no Modo Seguro; roteiro de recuperação pelo WinRE (`reg load` offline).
4. Tarefa agendada de reversão: execução sem sessão, após boot, ACL do binário/script, comportamento quando a máquina desliga antes do prazo; cmdlets e campos para conferir (habilitada, comando, `NextRunTime`); ACL restrita do backup em `C:\ProgramData\toolkit`.
5. Nomes das classes `{36fc9e60-...}` e `{88BAE032-...}` (a fonte [2] as usa nos cenários de USB) e se bloquear só elas impede o filho HID de um dispositivo novo; valor de registro da camada de "dispositivos removíveis"; GUIDs de Keyboard, Mouse, HIDClass e DiskDrive (só entram como o que o plano **nunca** bloqueia).
6. `Get-PnpDevice`/`Get-PnpDeviceProperty` (`DEVPKEY_Device_Parent`) e `pnputil /enum-devices /connected /ids /relations`: disponibilidade e formato por versão do Windows; leitura completa da árvore de pais (teclado I2C/PS2/Bluetooth, NVMe/SATA, dispositivo composto).
7. USBGuard: leitura de `rules.d`/permissões/dono e do estado `is-active`/`is-enabled` para o registro do desfazer; pacote no Arch (repositório oficial), unit do systemd por distro, permissões do `rules.conf`, subcomandos `list-devices`/`allow-device` (formato e opção de regra permanente), mecanismo de hash do dispositivo.
8. macOS: a opção "Allow accessories to connect" em desktops e em Macs Intel; leitura suportada da preferência.
9. Rede: formato de saída de cada comando por SO (`netsh wlan show interfaces`, `nmcli`, `iw`, `resolvectl`, `networksetup`, `scutil`, `wdutil`, `socketfilterfw`, `pfctl`), disponibilidade do `airport`, se `netsh wlan show networks` e o `system_profiler` forçam varredura (precisa ser passivo), `nmcli ... --rescan no` por versão, `Get-DnsClientDohServerAddress`, `Get-NetIPv6Protocol`, e lista final de portas sensíveis.
10. Otimização: nomes e tipos dos valores `MinAnimate`, `TaskbarAnimations`, `VisualFXSetting`; quais efeitos só valem após reiniciar o Explorer; evento 100 só com degradação, mapeamento de 101/102 e evento de desligamento 200 (o pedido cita 100/200), e se o log operacional exige administrador.
11. Licenças a confirmar na fonte do projeto: Brave (MPL-2.0), LibreOffice (MPL-2.0), PeaZip (LGPL-3.0).
12. ids a confirmar: cask do PeaZip, pacotes apt/dnf de LibreOffice, Microsoft Edge e Keka fora de escopo, id do RustDesk no winget (não encontrado no repositório em 2026-11-14: o Windows ficou sem instalador no PR13).
13. RustDesk: versão mínima verificada no PR13 (NVD, ver acima; sobram os CVEs com versão inferida e os avisos do GitHub do projeto), enforcement da versão mínima (PR futuro), modelo de criptografia, 2FA, abuso em golpes; Chrome Remote Desktop e Assistência Rápida (criptografia, exigência de conta); histórico de CVEs de TightVNC; qual linha (2.x GPL ou 4.x fechada) o id `GlavSoft.TightVNC` instala; incidente público do AnyDesk.

## Fontes

Todas consultadas em 2026-11-14. Páginas de agregadores de CVE e fóruns foram usadas só como indício e estão marcadas como tal.

1. Microsoft Learn, [Manage Device Installation with Group Policy](https://learn.microsoft.com/en-us/windows/client-management/manage-device-installation-with-group-policy) (cenários, IDs de hardware e compatíveis, opção retroativa nas políticas "Prevent", PnPUtil e Gerenciador de Dispositivos).
2. Microsoft Learn, [DeviceInstallation Policy CSP](https://learn.microsoft.com/en-us/windows/client-management/mdm/policy-csp-deviceinstallation) (nomes das políticas, valores de registro `AllowDeviceIDs`, `AllowInstanceIDs`, `AllowDeviceClasses`, `AllowDenyLayered`, `DenyUnspecified`, `DenyDeviceIDs`, `DenyInstanceIDs`, `DenyDeviceClasses`, edições Pro/Enterprise/Education/IoT, verificação em `setupapi.dev.log`).
3. USBGuard, [README no GitHub](https://github.com/USBGuard/usbguard) (licença GPL-2.0 ou posterior, `generate-policy`, `/etc/usbguard/rules.conf`, `--enable-systemd`, `systemctl start|enable usbguard.service`).
4. USBGuard, [manual do `usbguard-daemon.conf`](https://github.com/USBGuard/usbguard/blob/main/doc/man/usbguard-daemon.conf.5.adoc) (`ImplicitPolicyTarget`, `PresentDevicePolicy`, `InsertedDevicePolicy`, `PresentControllerPolicy`, `IPCAllowedUsers`, `RuleFile`).
5. Pacotes: [Debian Package Tracker, usbguard](https://tracker.debian.org/pkg/usbguard), [Ubuntu, usbguard (jammy)](https://packages.ubuntu.com/jammy/usbguard), [Fedora Packages, usbguard](https://packages.fedoraproject.org/pkgs/usbguard/usbguard/fedora-40.html). Arch: sem página oficial encontrada (A VERIFICAR).
6. Apple, [Allow USB and other accessories to connect to your Mac](https://support.apple.com/en-ng/102282) (opções, Macs notebook com Apple silicon, supervisão por Apple Configurator, desbloqueio e três dias).
7. Microsoft Learn, [Get-NetFirewallProfile](https://learn.microsoft.com/en-us/powershell/module/netsecurity/get-netfirewallprofile) (perfis Domain, Private e Public do firewall; lista de propriedades como `Enabled` não foi detalhada na página lida: A VERIFICAR).
8. Microsoft Learn, [Event ID 100 (Windows Diagnostics Performance), TechNet wiki arquivado](https://learn.microsoft.com/en-us/archive/technet-wiki/11559.event-id-100-windows-diagnostics-performance) e perguntas e respostas da comunidade sobre o evento ausente (indício, não oficial): [Diagnostics-performance event ID 100](https://learn.microsoft.com/en-us/answers/questions/2422685/diagnostics-perfomance-event-id-100-slow-boot).
9. Microsoft Learn, [SystemParametersInfoW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-systemparametersinfow) (`SPI_SETCLIENTAREAANIMATION`, `SPI_SETANIMATION`, `SPI_SETMENUANIMATION`, `SPI_SETCOMBOBOXANIMATION`, `SPI_SETLISTBOXSMOOTHSCROLLING`, `SPI_SETTOOLTIPANIMATION`, `SPI_SETUIEFFECTS`, `SPIF_UPDATEINIFILE`, `SPIF_SENDCHANGE`).
10. [RustDesk no GitHub](https://github.com/rustdesk/rustdesk) (licença AGPL-3.0 na API do GitHub), [RustDesk na Flathub](https://flathub.org/apps/com.rustdesk.RustDesk) (`com.rustdesk.RustDesk`, AGPL-3.0-only) e [cask `rustdesk` na API do Homebrew](https://formulae.brew.sh/api/cask/rustdesk.json).
11. Indícios (agregadores, não oficiais; confirmar no NVD e nos avisos do GitHub): [Cyber Security WA, RustDesk Critical Vulnerabilities (2026-03)](https://soc.cyber.wa.gov.au/advisories/20260309001-Rustdesk-Vulnerabilities-and-POC/), [CVE-2026-30783 (GCVE)](https://db.gcve.eu/vuln/CVE-2026-30783).
12. TightVNC, [Licensing](https://www.tightvnc.com/licensing.php) (TightVNC 2.x sob GPL, uso comercial livre; TightVNC 4.x de código fechado, versão base gratuita; criptografia TLS 1.3 só no Pro). A página não trata das versões 1.x e 3.x.
13. Repositórios públicos para ids (consultados pela API do GitHub e do Homebrew, sem executar gerenciador): [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) (`7zip.7zip`, `Brave.Brave`, `Google.Chrome`, `TheDocumentFoundation.LibreOffice`, `Giorgiotani.Peazip`, `GlavSoft.TightVNC`, `TigerVNC.TigerVNC`), [Homebrew formulae e casks](https://formulae.brew.sh) (casks `brave-browser`, `google-chrome`, `libreoffice`, `rustdesk`, `keka`; fórmula `sevenzip`), [Flathub](https://flathub.org) (`com.brave.Browser`, `com.google.Chrome`, `org.libreoffice.LibreOffice`, `io.github.peazip.PeaZip`, `com.rustdesk.RustDesk`, `org.tigervnc.vncviewer`, `org.gnome.FileRoller`).
14. NetworkManager, [nmcli](https://networkmanager.dev/docs/api/latest/nmcli.html) (`device wifi list`: por padrão a lista tem no máximo 30 s e dispara varredura se preciso; `--rescan auto|no|yes`).
15. Microsoft, [netsh wlan](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/netsh-wlan) (`show networks`: "redes visíveis ao alcance"; não diz se há varredura).
