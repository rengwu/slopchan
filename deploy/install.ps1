# Windows PowerShell 5.1+ / PowerShell 7. No Go, Docker, or administrator needed
# for foreground installation. Usage: .\install.ps1 [-Version vMAJOR.MINOR.PATCH] [-AtLogon]
[CmdletBinding()]
param(
    [ValidatePattern('^(latest|v[0-9]+\.[0-9]+\.[0-9]+)$')]
    [string]$Version = 'latest',
    [switch]$AtLogon
)
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$cpu = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $cpu = $env:PROCESSOR_ARCHITEW6432 }
switch ($cpu) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "Unsupported Windows CPU: $cpu. Use 64-bit Windows." }
}
$base = 'https://github.com/rengwu/slopchan/releases'
if ($Version -eq 'latest') {
    $release = Invoke-RestMethod 'https://api.github.com/repos/rengwu/slopchan/releases/latest'
    $Version = $release.tag_name
    if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'Latest release is not a stable version.' }
}
$archive = "slopchan_$($Version.Substring(1))_windows_$arch.zip"
$temp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
$root = Join-Path $env:LOCALAPPDATA 'slopchan'
$exe = Join-Path $root 'slopchan.exe'
$tokens = Join-Path $root 'tokens'
$data = Join-Path $root 'data'
New-Item -ItemType Directory -Force $temp | Out-Null
try {
    Invoke-WebRequest -UseBasicParsing "$base/download/$Version/$archive" -OutFile "$temp/$archive"
    Invoke-WebRequest -UseBasicParsing "$base/download/$Version/checksums.txt" -OutFile "$temp/checksums.txt"
    $lines = @(Get-Content "$temp/checksums.txt" | Where-Object { ($_ -split '\s+')[1] -ceq $archive })
    if ($lines.Count -ne 1) { throw 'Missing or duplicate checksum entry.' }
    $expected = ($lines[0] -split '\s+')[0]
    if ((Get-FileHash "$temp/$archive" -Algorithm SHA256).Hash -ine $expected) { throw 'SHA-256 verification failed.' }
    Expand-Archive "$temp/$archive" "$temp/unpacked"
    New-Item -ItemType Directory -Force $root, $data | Out-Null
    # Remove inherited ACLs and allow only this user and SYSTEM.
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    & icacls.exe $root /inheritance:r /grant:r "*${sid}:(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not protect the installation directory.' }
    if (!(Test-Path $tokens)) {
        $bytes = New-Object byte[] 32
        $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
        [IO.File]::WriteAllText($tokens, ([BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()))
    }
    # Windows locks running executables. Fail without killing an existing server.
    Copy-Item "$temp/unpacked/slopchan.exe" $exe -Force
    Copy-Item "$temp/unpacked/deploy", "$temp/unpacked/docs", "$temp/unpacked/licenses", "$temp/unpacked/skills" $root -Recurse -Force
    Copy-Item "$temp/unpacked/LICENSE", "$temp/unpacked/README.md", "$temp/unpacked/DESIGN.md", "$temp/unpacked/compose.yaml", "$temp/unpacked/compose.lan.yaml", "$temp/unpacked/.env.example" $root -Force
    if (Test-Path "$temp/unpacked/onboarding.md") { Copy-Item "$temp/unpacked/onboarding.md" $root -Force }
    if ($AtLogon) {
        . "$root/deploy/setup-windows-task.ps1"
        Start-SlopchanLogonTask -TaskName "slopchan-$sid" -Executable $exe -DataDirectory $data -TokenFile $tokens
    } else {
        Write-Host "Start with: & `"$exe`" serve -data `"$data`" -token-file `"$tokens`""
    }
    Write-Host "Installed $Version. Posting token: $tokens (preserved on upgrades)."
    Write-Host 'Next: configure admin credentials and HTTPS using docs/install.md#native-windows-x64-and-arm64.'
    Write-Host 'Then visit /admin, save the Public URL, and download a named token for your agent.'
} finally {
    Remove-Item $temp -Recurse -Force
}
