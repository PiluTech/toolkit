# Bootstrap do toolkit para Windows (PowerShell 5.1 ou 7): baixa o binario da release, confere o
# SHA-256 pelo checksums.txt da mesma release e instala em %LOCALAPPDATA%\toolkit\bin, que entra no
# PATH do usuario. Nao exige administrador. Arquivo so com ASCII, de proposito: o PowerShell 5.1 le
# arquivo sem BOM como ANSI.
#
#   irm https://github.com/PiluTech/toolkit/releases/latest/download/win.ps1 | iex
#   $env:TOOLKIT_DRYRUN = '1'; irm .../win.ps1 | iex     # so mostra o que faria
#   $env:TOOLKIT_VERSION = 'v0.1.0'                      # fixa a versao (padrao: a ultima release)
#   .\win.ps1 -DryRun                                    # equivalente, rodando o arquivo salvo

function Install-Toolkit {
    param([switch]$DryRun)
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
    $version = $env:TOOLKIT_VERSION
    if ($env:TOOLKIT_RELEASE_BASE) {
        $base = $env:TOOLKIT_RELEASE_BASE.TrimEnd('/')
        Write-Warning "toolkit: usando TOOLKIT_RELEASE_BASE=$base (so para testes)"
    } elseif (-not $version -or $version -eq 'latest') {
        $base = 'https://github.com/PiluTech/toolkit/releases/latest/download'
    } else {
        if ($version -notmatch '^[A-Za-z0-9._+-]+$') { throw 'toolkit: TOOLKIT_VERSION invalida: use algo como v0.1.0' }
        if ($version -match '^[0-9]') { $version = "v$version" }
        $base = "https://github.com/PiluTech/toolkit/releases/download/$version"
    }

    if ($env:TOOLKIT_INSTALL_DIR) { $dest = $env:TOOLKIT_INSTALL_DIR } else { $dest = Join-Path $env:LOCALAPPDATA 'toolkit\bin' }

    if ($DryRun) {
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
}

Install-Toolkit -DryRun:($env:TOOLKIT_DRYRUN -eq '1' -or ($args -contains '-DryRun'))
