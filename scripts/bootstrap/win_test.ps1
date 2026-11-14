# Teste do win.ps1 com release falsa num servidor HTTP local (HttpListener), checksums adulterados
# e dry-run. Roda no Windows PowerShell 5.1 ou no pwsh, sem instalar nada fora de um diretorio
# temporario (TOOLKIT_INSTALL_DIR + TOOLKIT_NO_PATH=1, o PATH real do usuario nao e tocado).
#   powershell -NoProfile -File scripts\bootstrap\win_test.ps1
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'win.ps1'

function Fail($m) { throw "FALHOU: $m" }

# Carrega so as funcoes puras do win.ps1 (sem rodar a instalacao), pela AST.
$ast = [Management.Automation.Language.Parser]::ParseFile($script, [ref]$null, [ref]$null)
foreach ($fn in $ast.FindAll({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -in 'Test-EnvOn', 'Test-ShouldLaunch', 'Start-ToolkitTui', 'Get-VersionArg', 'Test-VersionTag' }, $true)) {
    . ([scriptblock]::Create($fn.Extent.Text))
}

# Decisao de abrir a TUI: base interativa e console real; cada veto individual.
function Should($want, $what, $over) {
    $a = @{ NoLaunch = $false; NoLaunchEnv = ''; UserInteractive = $true; InputRedirected = $false; OutputRedirected = $false; Remote = $false; Env = @{} }
    foreach ($k in $over.Keys) { $a[$k] = $over[$k] }
    $got = Test-ShouldLaunch @a
    if ($got -ne $want) { Fail "Test-ShouldLaunch ($what): esperado $want, veio $got" }
}
Should $true 'sessao interativa com console' @{}
Should $true 'TOOLKIT_NO_LAUNCH=0' @{ NoLaunchEnv = '0' }
Should $true 'TOOLKIT_NO_LAUNCH=false' @{ NoLaunchEnv = 'false' }
Should $true 'CI vazio' @{ Env = @{ CI = '' } }
Should $true 'CI=0' @{ Env = @{ CI = '0' } }
Should $true 'CI=no' @{ Env = @{ CI = 'No' } }
Should $true 'CI=off com espacos' @{ Env = @{ CI = ' off ' } }
Should $true 'CI so espacos' @{ Env = @{ CI = '   ' } }
Should $true 'NO_LAUNCH=off' @{ NoLaunchEnv = 'OFF' }
Should $true 'NO_LAUNCH=0 com espacos' @{ NoLaunchEnv = ' 0 ' }
Should $true 'CI=false' @{ Env = @{ CI = 'FALSE' } }
Should $false '-NoLaunch' @{ NoLaunch = $true }
Should $false 'TOOLKIT_NO_LAUNCH=1' @{ NoLaunchEnv = '1' }
Should $false 'TOOLKIT_NO_LAUNCH=sim' @{ NoLaunchEnv = 'sim' }
Should $false 'CI' @{ Env = @{ CI = 'true' } }
Should $false 'GITHUB_ACTIONS' @{ Env = @{ GITHUB_ACTIONS = 'true' } }
Should $false 'TF_BUILD' @{ Env = @{ TF_BUILD = 'True' } }
Should $false 'CI=1' @{ Env = @{ CI = '1' } }
Should $false 'CODEBUILD_BUILD_ID' @{ Env = @{ CODEBUILD_BUILD_ID = 'x' } }
Should $false 'remoting' @{ Remote = $true }
Should $false 'sessao nao interativa' @{ UserInteractive = $false }
Should $false 'stdin redirecionado' @{ InputRedirected = $true }
Should $false 'stdout redirecionado' @{ OutputRedirected = $true }

# Versao pedida: -Versao > TOOLKIT_VERSION > padrao.
function Ver($want, $what, $argv, $envv) {
    $got = Get-VersionArg -ArgList $argv -EnvVersion $envv
    if ($got -ne $want) { Fail "Get-VersionArg ($what): esperado '$want', veio '$got'" }
}
Ver '' 'nada' @() ''
Ver '' 'so outras flags' @('-DryRun') $null
Ver 'v0.1.0' 'so env' @() 'v0.1.0'
Ver 'v0.4.0-rc.1' 'so -Versao' @('-Versao', 'v0.4.0-rc.1') ''
Ver 'v0.4.0-rc.1' '-Versao vence o env' @('-Versao', 'v0.4.0-rc.1') 'v0.1.0'
Ver 'v0.4.0-rc.1' '-versao minusculo' @('-DryRun', '-versao', 'v0.4.0-rc.1') ''
Ver 'v0.4.0-rc.1' '-Versao:valor' @('-Versao:v0.4.0-rc.1') ''
foreach ($bad in @(@('-Versao'), @('-Versao', ''), @('-Versao', '-DryRun'), @('-Versao:'))) {
    $threw = $false
    try { Get-VersionArg -ArgList $bad -EnvVersion 'v0.1.0' | Out-Null } catch { $threw = $true; if ($_.Exception.Message -notmatch '-Versao exige um valor') { Fail "mensagem de -Versao sem valor: $($_.Exception.Message)" } }
    if (-not $threw) { Fail "-Versao sem valor foi aceito: $($bad -join ' ')" }
}

# TUI que nao abre: nao lanca excecao e imprime a dica em pt-BR.
$msg = (Start-ToolkitTui -Exe (Join-Path ([IO.Path]::GetTempPath()) 'toolkit-inexistente.exe') 6>&1 | Out-String)
if ($msg -notmatch 'toolkit tui') { Fail "sem a dica de como abrir depois: $msg" }
if ($msg -notmatch 'instalacao terminou') { Fail "sem aviso de instalacao concluida: $msg" }

$root = Join-Path ([IO.Path]::GetTempPath()) ('toolkit-test-' + [Guid]::NewGuid().ToString('N'))
$rel = Join-Path $root 'release'
New-Item -ItemType Directory -Path $rel | Out-Null

$raw = $env:PROCESSOR_ARCHITEW6432
if (-not $raw) { $raw = $env:PROCESSOR_ARCHITECTURE }
$arch = if ($raw -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "toolkit_windows_$arch.exe"
$port = Get-Random -Minimum 20000 -Maximum 40000
$base = "http://127.0.0.1:$port"

$job = Start-Job -ArgumentList $rel, $port -ScriptBlock {
    param($dir, $port)
    $l = New-Object Net.HttpListener
    $l.Prefixes.Add("http://127.0.0.1:$port/")
    $l.Start()
    while ($l.IsListening) {
        $ctx = $l.GetContext()
        $f = Join-Path $dir ($ctx.Request.Url.AbsolutePath.TrimStart('/'))
        if (Test-Path -LiteralPath $f -PathType Leaf) {
            $b = [IO.File]::ReadAllBytes($f)
            $ctx.Response.OutputStream.Write($b, 0, $b.Length)
        } else { $ctx.Response.StatusCode = 404 }
        $ctx.Response.Close()
    }
}

function Write-Release($tamper) {
    [IO.File]::WriteAllBytes("$rel\$asset", [Text.Encoding]::ASCII.GetBytes('binario-falso'))
    $h = (Get-FileHash -Algorithm SHA256 -LiteralPath "$rel\$asset").Hash.ToLower()
    if ($tamper) { $h = '0' * 64 }
    [IO.File]::WriteAllText("$rel\checksums.txt", "$h  $asset`n")
}

# Roda o bootstrap em outro processo (o 'throw' vira exit code != 0); devolve $true se terminou bem.
function Invoke-Bootstrap($dest, $extra) {
    $env:TOOLKIT_RELEASE_BASE = $base
    $env:TOOLKIT_INSTALL_DIR = $dest
    $env:TOOLKIT_NO_PATH = '1'
    $exe = (Get-Process -Id $PID).Path
    & $exe -NoProfile -File $script @extra *> "$root\out.txt"
    return ($LASTEXITCODE -eq 0)
}

try {
    for ($i = 0; $i -lt 20; $i++) { try { Invoke-WebRequest -UseBasicParsing "$base/x" -ErrorAction Stop | Out-Null } catch { if ($_.Exception.Response) { break }; Start-Sleep -Milliseconds 300 } }

    # 1. caminho feliz
    Write-Release $false
    $d1 = "$root\ok"
    if (-not (Invoke-Bootstrap $d1 @())) { Fail "instalacao valida: $(Get-Content -Raw "$root\out.txt")" }
    if (-not (Test-Path "$d1\toolkit.exe")) { Fail 'binario nao instalado' }
    # saida redirecionada (como aqui): nao abre a TUI, e a mensagem de instalacao aparece.
    if ((Get-Content -Raw "$root\out.txt") -notmatch 'toolkit instalado em') { Fail 'sem mensagem de instalacao' }
    if ((Get-Content -Raw "$root\out.txt") -match 'TUI nao abriu') { Fail 'tentou abrir a TUI sem console' }

    # 2. checksums adulterado: falha e nao instala
    Write-Release $true
    $d2 = "$root\adulterado"
    if (Invoke-Bootstrap $d2 @()) { Fail 'checksums adulterado foi aceito' }
    if (Test-Path "$d2\toolkit.exe") { Fail 'instalou com hash divergente' }
    if ((Get-Content -Raw "$root\out.txt") -notmatch 'diverge') { Fail 'sem mensagem de divergencia' }

    # 3. asset ausente do checksums.txt
    Write-Release $false
    [IO.File]::WriteAllText("$rel\checksums.txt", ('a' * 64) + "  outro.exe`n")
    $d3 = "$root\ausente"
    if (Invoke-Bootstrap $d3 @()) { Fail 'asset fora do checksums.txt foi aceito' }
    if (Test-Path "$d3\toolkit.exe") { Fail 'instalou sem entrada no checksums.txt' }

    # 4. dry-run nao instala
    Write-Release $false
    $d4 = "$root\dry"
    if (-not (Invoke-Bootstrap $d4 @('-DryRun'))) { Fail 'dry-run falhou' }
    if (Test-Path "$d4\toolkit.exe") { Fail 'dry-run instalou' }

    # 4b. dry-run mostra a versao pedida por -Versao (que vence TOOLKIT_VERSION)
    $env:TOOLKIT_VERSION = 'v0.1.0'; Remove-Item Env:\TOOLKIT_RELEASE_BASE -ErrorAction SilentlyContinue
    $exe = (Get-Process -Id $PID).Path
    $outV = (& $exe -NoProfile -File $script -DryRun -Versao v0.4.0-rc.1 *>&1 | Out-String)
    Remove-Item Env:\TOOLKIT_VERSION
    if ($outV -notmatch 'releases/download/v0\.4\.0-rc\.1') { Fail "dry-run sem a versao do -Versao: $outV" }
    if ($outV -notmatch 'versao v0\.4\.0-rc\.1') { Fail "dry-run nao mostra a versao: $outV" }
    & $exe -NoProfile -File $script -DryRun -Versao *> "$root\out2.txt"
    if ($LASTEXITCODE -eq 0) { Fail '-Versao sem valor foi aceito' }

    # 4c. formato da tag (mesma regra do release.yml): v opcional + X.Y.Z + sufixo opcional; '.' e '..' nao passam
    foreach ($bad in @('.', '..', '...', 'v..', 'v1', 'v1.2', 'v1.2.3.', 'v1.2.3-', 'v1.2.3-rc..1', 'v1.2.3-.rc', 'v1.2.3+x', 'v1.2.3/x', 'x1.2.3', 'v1.2.3-rc.1/..')) {
        if (Test-VersionTag $bad) { Fail "tag invalida aceita: $bad" }
    }
    foreach ($good in @('v1.2.3', '1.2.3', 'v0.4.0-rc.1', 'v1.0.0-beta.2', 'v1.0.0-alpha')) {
        if (-not (Test-VersionTag $good)) { Fail "tag valida recusada: $good" }
    }

    # 5. versao invalida
    $env:TOOLKIT_VERSION = 'v1/../x'; Remove-Item Env:\TOOLKIT_RELEASE_BASE -ErrorAction SilentlyContinue
    $exe = (Get-Process -Id $PID).Path
    & $exe -NoProfile -File $script -DryRun *> "$root\out.txt"
    Remove-Item Env:\TOOLKIT_VERSION
    if ($LASTEXITCODE -eq 0) { Fail 'versao invalida foi aceita' }

    Write-Host 'ok win_test'
} finally {
    Stop-Job $job -ErrorAction SilentlyContinue; Remove-Job $job -Force -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
}
