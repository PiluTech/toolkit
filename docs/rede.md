# `toolkit rede`

Lê a rede da máquina **só para leitura, sem alterar nada**. Funciona sem administrador e, quando você abre o terminal como
administrador, lê um pouco mais (veja "Administrador" abaixo). Responde "o que está exposto e o que fazer". É a função 2
do [plano de segurança e TUI](plano-seguranca-e-tui.md) (PR8).

```sh
toolkit rede                       # resumo: o que está exposto e o que fazer
toolkit rede --completo            # resumo + relatório completo (todos os dados lidos)
toolkit rede --json                # o mesmo em JSON
toolkit rede --relatorio rede.txt  # grava resumo + relatório completo num arquivo NOVO (0600, nunca sobrescreve)
toolkit rede ip [--servico-ip URL] [--yes] [--dry-run]          # IP público, SÓ sob pedido (veja "IP público e portal cativo")
toolkit rede portal [--servico-portal URL] [--yes] [--dry-run]  # sondagem de portal cativo, SÓ sob pedido
```

O andamento ("Lendo a rede…") vai para o stderr; `--json` não escreve andamento. O comando sempre sai com 0 (achado ruim é
informação, não falha do comando). O idioma da saída vem de `TOOLKIT_LANG`, `LC_ALL`, `LC_MESSAGES` ou `LANG` (pt-BR por
padrão; en e es). Na TUI: área 4 Rede › Análise (veja `docs/tui.md`).

**Privacidade dos dados lidos.** IPs, MACs, nomes de rede (SSID) e processos aparecem como o sistema os informa, sem máscara
(decisão do Paulo para uma ferramenta de análise, como o diagnóstico). Por isso `--json` e `--relatorio` imprimem, no
stderr e ANTES de gravar, o aviso de que a saída contém dados da rede local. Nada é enviado a ninguém: o pacote não tem
código de rede (um teste reprova `net/http`, `net.Dial` e `os/exec` em `internal/rede`). Só os subcomandos `ip` e `portal`,
sob pedido, falam com a internet (próxima seção).

## O que NÃO faz (nunca)

- **Não varre** portas, hosts, a sub-rede, o roteador nem redes Wi-Fi vizinhas (nada de nmap, ping sweep, ARP scan).
  Wi-Fi: lê só a conexão ATUAL; o `nmcli` leva sempre `--rescan no` (lista em cache), e `iw ... scan`,
  `nmcli ... rescan`, `netsh wlan show networks` e `airport -s` não existem no pacote.
- **Não altera** rede, firewall, DNS, perfil nem Wi-Fi. Nenhum comando de escrita (lista fechada, em teste).
- **Não acessa a internet** por conta própria. `toolkit rede` (e `--completo`, `--json`, `--relatorio`) nunca faz requisição:
  o IP público e o portal cativo só existem sob pedido, nos subcomandos `ip` e `portal` e na TUI (`o` e `w`), com um serviço
  que VOCÊ informa e depois de uma confirmação que o nomeia. A análise padrão mostra `IP público: não consultado (só sob
  pedido)`.
- **Não lê senha de Wi-Fi** (nada de `key=clear`, `show-secrets`, Keychain): só o TIPO de segurança.
- **Não eleva**: nenhum comando leva `Elevate`. O que exigiria administrador vira "não lido" (`Skipped.NeedsAdmin`) com a
  instrução; o usuário abre o terminal elevado se quiser.
- Nenhum comando recebe host, IP ou alvo digitado: os comandos são constantes. O único dado do sistema que vira argumento é
  o nome da interface Wi-Fi do macOS, validado por formato (`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,31}$`: nunca começa com `-`).

## O que cada SO lê

Tudo passa pelo `exec.Runner` de leitura (`exec.ReadOnlyRunner`: um comando que não seja leitura ou que peça elevação é
recusado), com caminho absoluto (macOS e Windows) ou achado por `LookPath` (Linux), e `LC_ALL=C` nos comandos Unix. Cada
área é independente: uma que falha vira "não lido" (com o motivo) e o resto continua. Os formatos de saída são hipóteses de
trabalho confirmadas só com fixtures sintéticas e, no macOS, numa máquina real; a conferência por SO fica para o smoke em VM
(**A VERIFICAR**, `docs/smoke.md`).

| Item | Windows (PowerShell `Get-*`, só ASCII, JSON) | Linux | macOS |
|---|---|---|---|
| IPv4/IPv6 por interface, MAC | `Get-NetAdapter`, `Get-NetIPAddress` (`SuffixOrigin=Random` = temporário) | `ip -j addr` (`temporary`), `/sys/class/net/*/wireless` | `networksetup -listallhardwareports`, `ifconfig -a` (`temporary`) |
| Gateway (rota padrão) | `Get-NetRoute` (`0.0.0.0/0`, `::/0`) | `ip -j route show default`, `ip -j -6 ...` | `netstat -rn -f inet` e `inet6` |
| Perfil de rede | `Get-NetConnectionProfile` (Public, Private, Domain) | não existe equivalente | não existe equivalente |
| Tipo de segurança do Wi-Fi conectado | `netsh.exe wlan show interfaces` (conexão atual; texto localizado, classificado pelos VALORES) | `nmcli -t -f ACTIVE,SSID,SECURITY dev wifi list --rescan no` | `ipconfig getsummary <interface Wi-Fi>` (campo `Security`) |
| DNS e DoH/DoT | `Get-DnsClientServerAddress`; `Get-DnsClientDohServerAddress` quando existe | `resolvectl status` (`+DNSOverTLS`); sem ele, `/etc/resolv.conf` (DoT desconhecido) | `scutil --dns` (DoH/DoT desconhecido) |
| Portas em escuta (processo, endereço) | `Get-NetTCPConnection -State Listen`, `Get-NetUDPEndpoint`, `Get-Process` | `ss -H -t -u -l -n -p` | `netstat -an -p tcp` (conjunto de portas) + `lsof -nP -iTCP -sTCP:LISTEN` (nomes); só TCP |
| Firewall | `Get-NetFirewallProfile` (por perfil) | `ufw status`, `firewall-cmd --state`, `nft list ruleset` (só root) | `socketfilterfw --getglobalstate`; `pfctl -s info` (só root) |
| IPv6 privacy extensions | `Get-NetIPv6Protocol` (`UseTemporaryAddresses`) | `/proc/sys/net/ipv6/conf/<if>/use_tempaddr` (>= 1 conta como ligado) | `sysctl -n net.inet6.ip6.use_tempaddr` |

Decisões e desvios do plano, por SO:

- **macOS, Wi-Fi.** O plano citava `system_profiler SPAirPortDataType` e `wdutil info`. O `system_profiler` pode listar (e
  possivelmente varrer) redes vizinhas e o `wdutil` exige `sudo`; por isso o tipo de segurança sai de `ipconfig getsummary`,
  uma leitura passiva do que o configd já sabe (sem varredura, sem `sudo`). Só as linhas `SSID :` e `Security :` do nível
  da interface (2 espaços de recuo) são lidas; o resto (pacote DHCP em várias linhas) é ignorado. O macOS costuma devolver
  `SSID : <redacted>`: o nome aparece como "o sistema não informou o nome". Token de `Security` desconhecido vira
  "não verificado" (nunca "seguro"); sem o campo, vira "não lido" com a explicação (formato **A VERIFICAR** por versão).
  `ipconfig setverbose` e qualquer subcomando de escrita estão fora (teste de lista fechada).
- **macOS, portas.** O `lsof` sem administrador só vê os processos do próprio usuário, então "sem porta sensível no `lsof`"
  não seria um dado. O conjunto de portas vem do `netstat -an -p tcp` (lista todos os sockets em escuta) e o `lsof` só dá o
  NOME do processo; sem administrador, as portas sem processo aparecem como "processo não informado" e o resumo diz que
  o administrador leria os nomes. UDP não é lido no macOS (o `lsof -iUDP` lista centenas de sockets sem escuta).
- **Linux, Wi-Fi.** Só com NetworkManager (`nmcli`); com placa Wi-Fi e sem `nmcli` fica "não lido". O cache do
  NetworkManager pode estar velho ou vazio: sem rede ativa na lista, o item fica "não verificado" (a leitura não varre). As
  redes vizinhas do cache são descartadas na leitura (nunca vão ao relatório). `iw dev <if> link` não é usado: não informa
  a segurança.
- **Windows, Wi-Fi.** O `netsh` só roda se um adaptador 802.11 existir. A saída é localizada (en-US, pt-BR, es) e vem na
  página de código do console: só se confia nos VALORES (`WPA3`, `WPA2`, `TKIP`, `CCMP`, `Open`/`Aberto`/`Abierto`...) e no
  prefixo ASCII dos nomes dos campos (`Auth*`, `Autentic*`, `Cipher`, `Cifr*`). O que não for reconhecido vira
  "não verificado" com o texto original.
- **Windows, DoH.** `Get-DnsClientDohServerAddress` só existe no Windows recente; sem ele o DoH fica "não informado". Com
  ele, o DNS conta como criptografado só se o servidor tem modelo DoH com `AutoUpgrade` ligado (A VERIFICAR na VM se isso
  equivale a "em uso").

## Achados e severidades

Cinco gravidades (as do plano), com a palavra sempre ao lado do símbolo (`✗ alta`, `! média`, `- baixa`, `- info`,
`✓ ok`); na TUI e no CLI o "o que fazer" sai em pt, en e es. **"Sem dado" nunca vira "ok"**: o que não foi lido aparece
como informativo ou em "Não lidos", e o resumo nunca diz "nada de ruim" quando não leu nada (`nada-lido`).

| Id | Gravidade | Quando |
|---|---|---|
| `wifi-aberto` | alta | Wi-Fi conectado sem senha |
| `wifi-wep` | alta | WEP (ou cifra WEP numa autenticação aberta) |
| `wifi-wpa` | média | WPA, TKIP, ou modo misto WPA/WPA2 |
| `wifi-wpa2` | info | WPA2-AES (a transição WPA2/WPA3 conta como WPA2: aceita o mais fraco) |
| `wifi-wpa3` | ok | WPA3 |
| `wifi-corporativo` | info | 802.1X (o toolkit não avalia o servidor) |
| `wifi-desconhecido` | info | tipo não reconhecido (mostra o texto original) |
| `firewall-desligado` (Unix) | alta | todas as entradas lidas desligadas |
| `firewall-desligado-<perfil>` (Windows) | alta no perfil em uso, baixa no perfil sem uso | `Enabled=False` |
| `firewall-indeterminado` | info | algum firewall não pôde ser lido (muitos pedem root) e nenhum está ligado |
| `firewall-ligado` | ok | ligado (Unix: algum; Windows: os perfis em uso) |
| `porta-<n>` | ver abaixo | porta TCP da lista fechada escutando em endereço NÃO local |
| `portas-expostas` | info | uma linha com a contagem das demais portas TCP/UDP não locais |
| `perfil-publico-compartilhando` | média | perfil Public (Windows) e SMB (445/139) escutando em endereço não local |
| `perfil-privado-rede-aberta` | média | perfil Private (Windows) numa rede Wi-Fi aberta |
| `dns-publico` | baixa | DNS em endereço público, sem DoH/DoT |
| `dns-publico-nao-verificado` | info | DNS em endereço público, sem informação sobre DoH/DoT |
| `ipv6-sem-privacidade` | baixa | IPv6 global presente e endereços temporários desligados |
| `sem-gateway` | média | tabela de rotas lida e sem rota padrão |
| `ip-publico` | info | sem pedido: "não consultado (só sob pedido)"; com pedido: o IP e o serviço, ou "não foi possível obter" com o motivo |
| `maquina-exposta` | info, baixa ou média | só com o IP público consultado: cruza portas em escuta e firewall (ver "IP público e portal cativo") |
| `portal-cativo` | ok, baixa ou info | só com a sondagem pedida: "não detectado", "possível portal cativo" ou "sem conclusão" |
| `nada-lido` | info | nenhuma área devolveu dado |

**Portas sensíveis** (lista fechada em `internal/rede/analise.go`, só TCP; revisada neste PR, como o plano pediu): 21 FTP,
22 SSH, 23 Telnet, 135 RPC, 139 NetBIOS, 445 SMB, 1433 SQL Server, 2375 Docker sem TLS, 3306 MySQL, 3389 RDP, 5432
PostgreSQL, 5900 e 5901 VNC, 5985 e 5986 WinRM, 6379 Redis, 9200 Elasticsearch, 11211 Memcached, 27017 MongoDB. "Endereço não
local" é tudo que não é `127.0.0.0/8` nem `::1`: `0.0.0.0`, `::`, `*`, o IP da LAN, link-local, vazio ou ilegível (a dúvida
nunca esconde uma porta). Severidade: **risco alto** (21, 23, 2375, 5900, 5901, 6379, 9200, 11211, 27017) é **alta** com o
firewall desligado ou NÃO lido e **média** com ele ligado; **risco médio** (os demais) é **média** e **baixa**. Firewall
desconhecido nunca rebaixa. O texto diz "aceita conexões de fora desta máquina" e que se a internet alcança isto depende do
roteador, que o toolkit não vê: a função nunca afirma "você foi invadido". A mesma porta em IPv4 e IPv6 vira um achado só.

**DNS "público"** = fora de loopback, de RFC 1918, link-local, ULA, site-local (`fec0::/10`, que o Windows põe na
interface de loopback) e de CGNAT `100.64.0.0/10` (o DNS da Tailscale). Resolvedores de um domínio só (`Supplemental` do
macOS) e a lista "scoped" repetida do `scutil` são ignorados. Não há sondagem de "DNS inalcançável" (isso exigiria uma
consulta de rede): só "sem rota padrão".

## IP público e portal cativo (só sob pedido; PR9)

Decisões do plano aplicadas (#1 e #2): **nenhum serviço padrão** e **portal cativo junto do IP público**, ambos só com URL
informada pela pessoa. Descobrir o IP público exige uma requisição a um serviço externo, que vê o seu IP; por isso nada disso
roda na análise padrão.

**Como configurar.** Variável `TOOLKIT_IP_URL` (IP público) e `TOOLKIT_PORTAL_URL` (portal cativo), ou as flags
`--servico-ip` e `--servico-portal` (a flag vale mais). A URL passa pela mesma validação do catálogo: `https`, host público
com ponto, porta 443, sem usuário e senha, sem `#`, sem IP literal nem `localhost`, sem espaço, controle ou texto não ASCII
(use a forma punycode). URL recusada: nenhuma requisição e o erro não repete a URL (pode ter token). **Sem URL**, o pedido
responde "nenhum serviço configurado" e ensina a configurar (sai com 0, nada é enviado). Exemplos:

```sh
TOOLKIT_IP_URL=https://exemplo.org/ip toolkit rede ip      # o serviço deve devolver só o seu IP em texto
toolkit rede portal --servico-portal https://exemplo.org/gen204   # o endereço deve responder 204 sem conteúdo
```

**Confirmação (privacidade).** Antes de qualquer requisição o comando (e a TUI, na confirmação com foco em Cancelar) mostra:
o serviço (host e caminho; os parâmetros da URL não são mostrados, só avisados), o que será enviado (um GET, sem corpo, sem
cookies e sem cabeçalho de identificação além do `User-Agent: toolkit`), o que você revela (o IP público desta conexão ao
serviço e a quem o opera) e que nada sai sem a sua confirmação. No CLI: `[s/N]`, ou `--yes`; sem terminal exige `--yes`
(erro antes de enviar). `--dry-run` mostra isso e para: nada é enviado nem lido. A confirmação da TUI envia SÓ o plano
exibido (uso único): se a configuração mudar depois, o envio continua sendo o do serviço mostrado.

**A requisição** (`internal/rede/httpclient`, a única parte com `net/http`): TLS 1.2+ com o certificado sempre verificado,
sem proxy implícito (variáveis `HTTP(S)_PROXY` não valem), sem cookies, sem seguir redirecionamento (devolve o 3xx), sem
compressão nem conexão reaproveitada, prazo de 5 s, corpo limitado a 1 KiB (mais que isso é "resposta inesperada") e recusa
conectar a endereço não público (um nome que resolva para a rede local, loopback ou CGNAT: proteção contra SSRF). Nada é
gravado em disco.

**IP público.** A resposta tem de ser SÓ um endereço IPv4 ou IPv6 (`netip`), público (não loopback, privado, link-local,
CGNAT, não especificado nem multicast) e sem zona; qualquer outra coisa é "resposta inesperada" e o corpo nunca é mostrado
(nem limpo). Redirecionamento, HTTP diferente de 200, lixo, vazio e resposta grande também são "resposta inesperada".

**Máquina exposta?** Com o IP consultado, o achado `maquina-exposta` cruza as portas sensíveis em escuta em endereço não local
com o estado do firewall, que a leitura da análise já tem (severidade: média com firewall desligado ou desconhecido, baixa
com ele ligado, info sem porta sensível). O texto é honesto: **"o toolkit não testa de fora; só cruza o que está escutando
com o firewall"**: nunca varre o IP público nem o roteador e não afirma que a internet alcança a porta. Se o IP da interface
Wi-Fi ou de cabo é CGNAT (`100.64.0.0/10`), diz que a máquina provavelmente não é alcançável de fora por IPv4 (uma interface
sem tipo, como uma VPN nessa faixa, não prova CGNAT); com IPv6 global, avisa que ele pode ser alcançável. Se o IP público é o
de uma interface, diz "sem NAT". Sem a leitura das portas, diz "não foi possível avaliar".

**Portal cativo.** Um GET ao endereço de teste: **204** = não detectado; **redirecionamento (não seguido)**, **200** com
conteúdo ou **511** = "possível portal cativo" (mostra só o host do `Location`, limpo; o toolkit não abre nada: abra o
navegador num site http simples para ver a página de entrada); outro HTTP = sem conclusão; sem resposta = falha (não é prova
de portal nem de internet livre). Um serviço que responde 200 em vez de 204 aparece como "possível portal": use um endereço
de teste que responda 204.

**Segurança (modo guia do `security-audit`).** SSRF-like: a URL é informada pela pessoa e validada (https, host público, 443,
sem userinfo, fragmento nem IP literal), o cliente recusa endereço não público, não segue redirecionamento, limita corpo e
prazo, e nada é enviado sem confirmação; sem URL nada é enviado. Teste: `internal/rede/publico_test.go`,
`internal/rede/httpclient/client_test.go` (servidor `httptest` em loopback, nunca a internet), `internal/cli/rede_publico_test.go`
e `internal/tui/rede_consulta_test.go` (cliente falso que conta as chamadas).

## Administrador

Nenhum comando eleva. Com administrador (`sudo` no Linux e macOS; terminal como administrador no Windows) a análise lê a mais:

| Área | Sem administrador | Com administrador |
|---|---|---|
| Firewall (Linux) | `ufw status` e `firewall-cmd --state` falham sem root: ficam "não informado" e o `nft` nem é chamado | lê os três; `nft list ruleset` só roda com root |
| Firewall (macOS) | só o firewall de aplicativos; o `pf` fica "não lido" (vem desligado por padrão) | lê também `pfctl -s info` |
| Processos das portas (Linux, macOS) | só os do próprio usuário (`processos-das-portas`) | todos |
| Windows | nada a mais: o nome do processo vem do `Get-Process` | nada a mais |

## Limites honestos

- Sem varredura, "exposto" é inferência local: a função não vê o roteador nem o NAT, então não sabe se a internet alcança
  uma porta. Tampouco vê regras do firewall por porta (só se está ligado).
- O Wi-Fi mostra a rede conectada, não a "qualidade" do roteador (WPS, senha fraca). WPA2-AES sai como informativo.
- Perfil Public/Private do Windows: "compartilhamento ligado" é heurística (SMB escutando em endereço não local).
- Fora do macOS testado, as saídas reais por SO (`netsh`, `nmcli`, `resolvectl`, `Get-DnsClientDohServerAddress`,
  `Get-NetIPv6Protocol`, versões do macOS) estão **A VERIFICAR** no smoke de VM; as fixtures são sintéticas
  (IPs de documentação, MACs `02:00:00:00:00:xx`, SSID `REDE-FAKE`).
- UDP só no Windows e no Linux. DNS inalcançável fica fora (exigiria uma consulta de rede).
- IP público e portal cativo dependem de um serviço que a pessoa escolhe; o toolkit não avalia a política de privacidade dele.

## Código

`internal/rede`: `types.go` (Report, Interface, Addr, WiFi, Profile, DNS, Listener, Firewall, Finding, Tri, Security,
Severity), `deps.go` (`Deps{Runner, ReadFile, Glob, Now, Lang, Admin}`, `Collect(ctx, goos, Deps)`), `coleta_win.go`,
`coleta_lin.go`, `coleta_mac.go`, `analise.go` (`Analyze`, `Summary`), `relatorio.go` (`Full`), `publico.go` (consultas sob
pedido: `NewPlan`, `Plan.Describe`, `QueryPublicIP`, `ProbePortal`, `Fetcher`, `QuerySummary`; sem `net/http`) e
`httpclient/` (o `Fetcher` real). Testes: fixtures sintéticas
por SO, `contrato_test.go` (todos os comandos ReadOnly, sem Elevate, em lista fechada, sem termos de varredura, scripts do
PowerShell só com `Get-*`, pacote sem rede, coleta que não conhece a consulta), fronteiras de severidade. CLI: `internal/cli/rede.go` (`RedeDeps`); TUI:
`internal/tui/rede_tab.go` e `internal/cli/tui_rede.go`; subcomandos `ip` e `portal` em `internal/cli/rede_publico.go`
(`RedeDeps.Fetcher`).
