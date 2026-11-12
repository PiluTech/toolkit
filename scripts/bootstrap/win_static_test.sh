#!/usr/bin/env bash
# Conferencias estaticas do win.ps1 que rodam em qualquer SO (sem PowerShell no gate local). A execucao
# de verdade fica em win_test.ps1, para o smoke no Windows 10/11.
set -euo pipefail
cd "$(dirname "$0")"
f=win.ps1
fail() { echo "FALHOU (win.ps1): $*" >&2; exit 1; }
need() { grep -qE -- "$1" "$f" || fail "faltou: $2"; }
deny() { if grep -nE -- "$1" "$f" | grep -vE '^[0-9]+:\s*#' >/dev/null; then fail "proibido: $2"; fi; }

# So ASCII: o PowerShell 5.1 le arquivo sem BOM como ANSI e estragaria os acentos.
if LC_ALL=C grep -n '[^ -~	]' "$f" >/dev/null; then fail "caractere fora do ASCII"; fi

need 'Set-StrictMode -Version Latest' 'StrictMode'
need 'SecurityProtocolType\]::Tls12' 'TLS 1.2'
need 'Get-FileHash -Algorithm SHA256' 'conferencia SHA-256'
need 'checksums\.txt' 'checksums.txt'
need 'PROCESSOR_ARCHITEW6432' 'arquitetura de processo 32 bits'
need 'LOCALAPPDATA' 'instalacao no perfil do usuario'
need 'CurrentUser\.OpenSubKey\(.Environment., [$]true\)' 'PATH do usuario pelo registro (HKCU Environment)'
need 'DoNotExpandEnvironmentNames' 'ler o PATH sem expandir %VAR%'
need 'RegistryValueKind\]::ExpandString' 'preservar o tipo REG_EXPAND_SZ'
need 'ExpandEnvironmentVariables' 'comparar entradas ja expandidas'
deny "SetEnvironmentVariable\('Path'" 'SetEnvironmentVariable no Path acharia o %VAR% do PATH do usuario'
need 'github\.com/PiluTech/toolkit/releases' 'repositorio PiluTech/toolkit'
need 'TOOLKIT_VERSION' 'TOOLKIT_VERSION'
need '-DryRun' 'modo dry-run'

deny '\bexit\b' "'exit' fecharia o terminal de quem usa irm | iex"
deny 'Invoke-Expression|\biex\b' 'avaliar texto baixado'
deny "'Machine'|ProgramData|RunAs|-Verb" 'caminho de administrador'
deny 'ignore-security|-SkipCertificateCheck|ServerCertificateValidationCallback' 'desligar verificacao'
deny 'PiluVitu' 'repositorio antigo'

echo "ok win_static_test"
