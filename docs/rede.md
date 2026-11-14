# `toolkit rede`

Lê a rede da máquina **só para leitura, sem alterar nada**. Funciona sem administrador e, quando você abre o terminal como
administrador, lê um pouco mais (veja "Administrador" abaixo). Responde "o que está exposto e o que fazer". É a função 2
do [plano de segurança e TUI](plano-seguranca-e-tui.md) (PR8).

```sh
toolkit rede                       # resumo: o que está exposto e o que fazer
toolkit rede --completo            # resumo + relatório completo (todos os dados lidos)
toolkit rede --json                # o mesmo em JSON
toolkit rede --relatorio rede.txt  # grava resumo + relatório completo num arquivo NOVO (0600, nunca sobrescreve)
```

O andamento ("Lendo a rede…") vai para o stderr; `--json` não escreve andamento. O comando sempre sai com 0 (achado ruim é
informação, não falha do comando). O idioma da saída vem de `TOOLKIT_LANG`, `LC_ALL`, `LC_MESSAGES` ou `LANG` (pt-BR por
padrão; en e es). Na TUI: área 4 Rede › Análise (veja `docs/tui.md`).

**Privacidade dos dados lidos.** IPs, MACs, nomes de rede (SSID) e processos aparecem como o sistema os informa, sem máscara
(decisão do Paulo para uma ferramenta de análise, como o diagnóstico). Por isso `--json` e `--relatorio` imprimem, no
stderr e ANTES de gravar, o aviso de que a saída contém dados da rede local. Nada é enviado a ninguém: o pacote não tem
código de rede (um teste reprova `net/http`, `net.Dial` e `os/exec` em `internal/rede`).

## O que NÃO faz (nunca)

- **Não varre** portas, hosts, a sub-rede, o roteador nem redes Wi-Fi vizinhas (nada de nmap, ping sweep, ARP scan).
  Wi-Fi: lê só a conexão ATUAL; o `nmcli` leva sempre `--rescan no` (lista em cache), e `iw ... scan`,
  `nmcli ... rescan`, `netsh wlan show networks` e `airport -s` não existem no pacote.
- **Não altera** rede, firewall, DNS, perfil nem Wi-Fi. Nenhum comando de escrita (lista fechada, em teste).
- **Não acessa a internet** e não consulta o IP público (decisão 1 do plano: nenhum serviço nesta fase; o PR9 trata, sob
  pedido). A análise mostra `IP público: não consultado (sob pedido, PR seguinte)` como item informativo. O portal cativo
  também fica de fora (decisão 2).
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
  `SSID : <redacted>`: o nome aparece como "o sistema não informou o nome". "Aberto" só com o token explícito `NONE` ou
  `OPEN`; `Security` vazio, só pontuação ou token desconhecido vira "não verificado" (nunca "aberto" nem "seguro", porque
  risco alto exige evidência); sem o campo, vira "não lido" com a explicação (formato **A VERIFICAR** por versão).
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
- **Windows, status por subconsulta.** O PowerShell engole o erro de um `Get-*` e sai com código 0, então "lista vazia" não
  provaria nada. Cada consulta roda num `try/catch` com `-ErrorAction Stop` e o script devolve, por subconsulta,
  `{ok:true,data:[...]}` ou `{ok:false,err:"<tipo do erro>"}` (só o tipo, nunca a mensagem). O parser só aceita `ok`
  booleano verdadeiro com `data` presente; campo ausente, `ok` como texto ou subconsulta ausente é FALHA. Vazio legítimo
  (`ok:true`, `data` vazio) continua sendo leitura (sem rota padrão, nenhuma porta). Falha vira `Skipped` da área
  (`rotas`, `portas`, `interfaces`, `perfil`, `firewall`, `dns`, `ipv6`) citando o cmdlet e o tipo do erro, e NUNCA vira
  "sem gateway", "nenhuma porta escutando" nem achado de offline. As portas só contam como lidas com TCP e UDP ok; as
  interfaces só entram com adaptadores e IPs ok. As rotas e as conexões TCP são lidas por `Get-NetRoute`/
  `Get-NetTCPConnection` sem filtro e filtradas no pipeline (os filtros do cmdlet erram quando nada casa). **A VERIFICAR** na VM.
- **DNS criptografado: modo e fallback.** O modelo guarda o MODO configurado (`DNS.Mode`: `strict`, `opportunistic`, `off`,
  `unknown`) e se há fallback para DNS em claro (`DNS.AllowFallback`, tri-estado). `Encrypted=Yes` só quando a configuração
  prova: Windows = DoH com `AutoUpgrade` E `AllowFallbackToUdp=false` em todos os servidores; Linux = `DNSOverTLS=yes`
  (`+DNSOverTLS`). `AutoUpgrade` com fallback permitido, ou com o fallback não lido, e o DoT `opportunistic` do
  `resolvectl` ficam `Encrypted=Unknown` com modo `opportunistic` e o texto "configurado para tentar DoH/DoT; pode cair para
  DNS em claro" (ou "fallback não lido"); sem DoH/DoT configurado, `No`. Nunca há consulta externa para confirmar. O
  cmdlet de DoH ausente (Windows antigo) deixa tudo "não informado"; o cmdlet presente que falha vira `Skipped`. **A
  VERIFICAR** na VM o nome exato das propriedades `AutoUpgrade`/`AllowFallbackToUdp` e, no Linux, como o `resolvectl`
  imprime o modo oportunista (`DNSOverTLS=opportunistic`).

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
| `dns-publico-nao-verificado` | info | DNS em endereço público, sem informação sobre DoH/DoT, ou DoH/DoT oportunista (pode usar DNS em claro) |
| `ipv6-sem-privacidade` | baixa | IPv6 global presente e endereços temporários desligados |
| `sem-gateway` | média | tabela de rotas lida e sem rota padrão |
| `ip-publico` | info | sempre: "não consultado (sob pedido, PR seguinte)" |
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
- UDP só no Windows e no Linux. IP público, portal cativo e DNS inalcançável ficam fora desta fase.

## Código

`internal/rede`: `types.go` (Report, Interface, Addr, WiFi, Profile, DNS, Listener, Firewall, Finding, Tri, Security,
Severity), `deps.go` (`Deps{Runner, ReadFile, Glob, Now, Lang, Admin}`, `Collect(ctx, goos, Deps)`), `coleta_win.go`,
`coleta_lin.go`, `coleta_mac.go`, `analise.go` (`Analyze`, `Summary`), `relatorio.go` (`Full`). Testes: fixtures sintéticas
por SO, `contrato_test.go` (todos os comandos ReadOnly, sem Elevate, em lista fechada, sem termos de varredura, scripts do
PowerShell só com `Get-*`, pacote sem rede), fronteiras de severidade. CLI: `internal/cli/rede.go` (`RedeDeps`); TUI:
`internal/tui/rede_tab.go` e `internal/cli/tui_rede.go`.
