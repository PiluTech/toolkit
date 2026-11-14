# Bootstrap do toolkit para Windows (PowerShell 5.1 ou 7): baixa o binario da release, confere o
# SHA-256 pelo checksums.txt da mesma release e instala em %LOCALAPPDATA%\toolkit\bin, que entra no
# PATH do usuario. Nao exige administrador. Arquivo so com ASCII, de proposito: o PowerShell 5.1 le
# arquivo sem BOM como ANSI.
#
#   irm https://github.com/PiluTech/toolkit/releases/latest/download/win.ps1 | iex
#   $env:TOOLKIT_DRYRUN = '1'; irm .../win.ps1 | iex     # so mostra o que faria
#   $env:TOOLKIT_VERSION = 'v0.1.0'                      # fixa a versao (padrao: a ultima release)
#   & ([scriptblock]::Create((irm https://pilutech.com.br/win))) -Versao v0.4.0-rc.1   # fixa a versao por parametro
#   .\win.ps1 -Versao v0.4.0-rc.1                        # equivalente, rodando o arquivo salvo
#   (precedencia: -Versao > TOOLKIT_VERSION > ultima release)
#   .\win.ps1 -DryRun                                    # equivalente, rodando o arquivo salvo
#   $env:TOOLKIT_NO_LAUNCH = '1'                         # nao abre a TUI depois de instalar
#   .\win.ps1 -NoLaunch                                  # equivalente, rodando o arquivo salvo
#
# Depois de instalar com sucesso, abre 'toolkit tui' na propria janela, so se a sessao for interativa
# e tiver console real (nunca em CI, remoting ou com stdin/stdout redirecionados).

# Mesma regra do envOn do Go: ligado = definido e, sem espacos e sem distinguir maiusculas, diferente de
# vazio, 0, false, no e off.
function Test-EnvOn {
    param([string]$Value)
    if ($null -eq $Value) { return $false }
    $v = $Value.Trim().ToLowerInvariant()
    return -not ($v -eq '' -or $v -eq '0' -or $v -eq 'false' -or $v -eq 'no' -or $v -eq 'off')
}

# Formato real da tag (mesma regra do release.yml): v opcional + X.Y.Z + sufixo opcional '-rc.1' (letras, numeros e
# pontos, sem ponto no comeco, no fim nem repetido). '.' e '..' nao passam. Pura (testavel sem Windows).
function Test-VersionTag {
    param([string]$Tag)
    return ($Tag -cmatch '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?\z')
}

# Versao pedida: '-Versao <tag>' nos argumentos do script vence TOOLKIT_VERSION, que vence o padrao
# (vazio = ultima release). Pura (testavel sem Windows). '-Versao' sem valor lanca erro claro (o 'throw'
# nao fecha o terminal de quem usou 'irm | iex'). Aceita tambem '-Versao:v1'.
function Get-VersionArg {
    param([object[]]$ArgList = @(), [string]$EnvVersion)
    $found = $false
    $val = $null
    for ($i = 0; $i -lt $ArgList.Count; $i++) {
        $a = [string]$ArgList[$i]
        if ($a -ieq '-Versao') {
            $found = $true
            if ($i + 1 -lt $ArgList.Count) { $val = [string]$ArgList[$i + 1] } else { $val = $null }
            if ($null -ne $val -and $val.StartsWith('-')) { $val = $null }
            break
        }
        if ($a -imatch '^-Versao:(.*)$') {
            $found = $true
            $val = $Matches[1]
            break
        }
    }
    if ($found) {
        if (-not $val -or -not $val.Trim()) { throw 'toolkit: -Versao exige um valor, por exemplo: -Versao v0.4.0-rc.1' }
        return $val.Trim()
    }
    if ($EnvVersion) { return $EnvVersion }
    return ''
}

# Decisao pura (fatos injetados, testavel sem Windows): $true se deve abrir a TUI.
function Test-ShouldLaunch {
    param(
        [bool]$NoLaunch,
        [string]$NoLaunchEnv,
        [bool]$UserInteractive,
        [bool]$InputRedirected,
        [bool]$OutputRedirected,
        [bool]$Remote,
        [hashtable]$Env = @{}
    )
    if ($NoLaunch) { return $false }
    if (Test-EnvOn $NoLaunchEnv) { return $false }
    foreach ($n in 'CI', 'GITHUB_ACTIONS', 'TF_BUILD', 'JENKINS_URL', 'GITLAB_CI', 'BUILDKITE', 'CIRCLECI', 'TEAMCITY_VERSION', 'APPVEYOR', 'TRAVIS', 'CODEBUILD_BUILD_ID') {
        if ($Env.ContainsKey($n) -and (Test-EnvOn ([string]$Env[$n]))) { return $false }
    }
    if ($Remote) { return $false }
    if (-not $UserInteractive) { return $false }
    if ($InputRedirected -or $OutputRedirected) { return $false }
    return $true
}

# Abre a TUI na propria janela pelo caminho completo (o PATH desta sessao pode nao ter o binario).
# Falha ao abrir ou saida com erro nunca derruba a instalacao: so avisa como abrir depois.
function Start-ToolkitTui {
    param([string]$Exe)
    $ok = $false
    try {
        & $Exe tui
        $ok = ($LASTEXITCODE -eq 0)
    } catch {
        $ok = $false
    }
    if (-not $ok) {
        Write-Host "A instalacao terminou, mas a TUI nao abriu. Para abrir depois, rode: toolkit tui"
    }
}

function Install-Toolkit {
    param([switch]$DryRun, [switch]$NoLaunch, [string]$Version)
    Set-StrictMode -Version Latest
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'   # o 5.1 fica muito lento com a barra de progresso

    # Os 'throw' abaixo nao fecham o terminal de quem usou 'irm | iex' (nada de 'exit').
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    # Processo 32 bits em sistema 64: PROCESSOR_ARCHITEW6432 traz a arquitetura real.
    $raw = $env:PROCESSOR_ARCHITEW6432
    if (-not $raw) { $raw = $env:PROCESSOR_ARCHITECTURE }
    switch ($raw) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "toolkit: arquitetura nao suportada: $raw" }
    }
    $asset = "toolkit_windows_$arch.exe"

    # TOOLKIT_RELEASE_BASE existe so para testes locais.
    $version = $Version
    if ($env:TOOLKIT_RELEASE_BASE) {
        $base = $env:TOOLKIT_RELEASE_BASE.TrimEnd('/')
        Write-Warning "toolkit: usando TOOLKIT_RELEASE_BASE=$base (so para testes)"
    } elseif (-not $version -or $version -eq 'latest') {
        $base = 'https://github.com/PiluTech/toolkit/releases/latest/download'
    } else {
        if (-not (Test-VersionTag $version)) { throw 'toolkit: versao invalida (-Versao ou TOOLKIT_VERSION): use algo como v0.1.0 ou v0.4.0-rc.1' }
        if ($version -match '^[0-9]') { $version = "v$version" }
        $base = "https://github.com/PiluTech/toolkit/releases/download/$version"
    }

    if ($env:TOOLKIT_INSTALL_DIR) { $dest = $env:TOOLKIT_INSTALL_DIR } else { $dest = Join-Path $env:LOCALAPPDATA 'toolkit\bin' }

    if ($DryRun) {
        if (-not $version -or $version -eq 'latest') { $shown = 'latest (ultima release)' } else { $shown = $version }
        Write-Host "toolkit (dry-run): versao $shown"
        Write-Host "toolkit (dry-run): baixaria $base/$asset e $base/checksums.txt"
        Write-Host "toolkit (dry-run): conferiria o SHA-256 e instalaria em $dest\toolkit.exe (e no PATH do usuario)"
        return
    }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('toolkit-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile "$tmp\$asset"
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile "$tmp\checksums.txt"

        # Formato do sha256sum: "<hash>  <arquivo>" (o "*" do modo binario e aceito).
        $want = $null
        foreach ($line in (Get-Content -LiteralPath "$tmp\checksums.txt")) {
            $p = $line.Trim() -split '\s+', 2
            if ($p.Count -eq 2 -and $p[1].TrimStart('*') -ceq $asset) { $want = $p[0]; break }
        }
        if (-not $want) { throw "toolkit: checksums.txt nao lista $asset; nada foi instalado" }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath "$tmp\$asset").Hash
        if ($got -ine $want) { throw "toolkit: SHA-256 diverge para $asset (esperado $want, obtido $got); nada foi instalado" }

        New-Item -ItemType Directory -Force -Path $dest | Out-Null
        Copy-Item -LiteralPath "$tmp\$asset" -Destination "$dest\toolkit.exe.new" -Force
        Move-Item -LiteralPath "$dest\toolkit.exe.new" -Destination "$dest\toolkit.exe" -Force
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
    Write-Host "toolkit instalado em $dest\toolkit.exe (SHA-256 conferido)"

    # PATH do usuario (nao o da maquina); TOOLKIT_NO_PATH=1 pula (testes). Le e grava o valor CRU do
    # registro (HKCU\Environment), sem expandir %VAR%: [Environment]::GetEnvironmentVariable expandiria
    # e SetEnvironmentVariable gravaria o resultado achatado, estragando entradas como %USERPROFILE%\bin.
    # O tipo ExpandString e preservado para o Windows continuar expandindo.
    if ($env:TOOLKIT_NO_PATH -ne '1') {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $have = @($userPath -split ';' | Where-Object { $_ } | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') })
            if ($have -inotcontains $dest.TrimEnd('\')) {
                if ($userPath) { $new = $userPath.TrimEnd(';') + ';' + $dest } else { $new = $dest }
                $key.SetValue('Path', $new, [Microsoft.Win32.RegistryValueKind]::ExpandString)
                # Gravar direto no registro nao avisa o Explorer: remover uma variavel de usuario que nao
                # existe dispara o WM_SETTINGCHANGE, e terminais novos ja enxergam o PATH.
                [Environment]::SetEnvironmentVariable('TOOLKIT_PATH_REFRESH', $null, 'User')
                Write-Host "Adicionado ao PATH do usuario; abra um terminal novo para usar 'toolkit'."
            }
        } finally {
            $key.Close()
        }
        if (($env:Path -split ';') -inotcontains $dest) { $env:Path = "$env:Path;$dest" }
    }

    # Instalacao concluida (a mensagem acima ja saiu): abre a TUI se a sessao permitir.
    $remote = Test-Path -LiteralPath variable:PSSenderInfo
    $ci = @{}
    foreach ($n in 'CI', 'GITHUB_ACTIONS', 'TF_BUILD', 'JENKINS_URL', 'GITLAB_CI', 'BUILDKITE', 'CIRCLECI', 'TEAMCITY_VERSION', 'APPVEYOR', 'TRAVIS', 'CODEBUILD_BUILD_ID') {
        $ci[$n] = [Environment]::GetEnvironmentVariable($n)
    }
    try {
        $inRedir = [Console]::IsInputRedirected
        $outRedir = [Console]::IsOutputRedirected
    } catch {
        $inRedir = $true
        $outRedir = $true
    }
    if (Test-ShouldLaunch -NoLaunch ([bool]$NoLaunch) -NoLaunchEnv $env:TOOLKIT_NO_LAUNCH -UserInteractive ([Environment]::UserInteractive) -InputRedirected $inRedir -OutputRedirected $outRedir -Remote $remote -Env $ci) {
        Start-ToolkitTui -Exe "$dest\toolkit.exe"
    }
}

Install-Toolkit -Version (Get-VersionArg -ArgList $args -EnvVersion $env:TOOLKIT_VERSION) -DryRun:($env:TOOLKIT_DRYRUN -eq '1' -or ($args -contains '-DryRun')) -NoLaunch:($args -contains '-NoLaunch')
