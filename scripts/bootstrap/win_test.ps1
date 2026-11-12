# Teste do win.ps1 com release falsa num servidor HTTP local (HttpListener), checksums adulterados
# e dry-run. Roda no Windows PowerShell 5.1 ou no pwsh, sem instalar nada fora de um diretorio
# temporario (TOOLKIT_INSTALL_DIR + TOOLKIT_NO_PATH=1, o PATH real do usuario nao e tocado).
#   powershell -NoProfile -File scripts\bootstrap\win_test.ps1
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'win.ps1'

function Fail($m) { throw "FALHOU: $m" }

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
