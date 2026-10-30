# Run the product and real service scenarios in an actual, isolated WSL2 distro.
# Only a fixed public summary is exported; Linux runtime/logs never leave the VHD.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$PSNativeCommandUseErrorActionPreference = $false
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or $env:RUNNER_ARCH -ne 'X64') {
    throw 'WSL acceptance requires a disposable GitHub-hosted Windows x64 runner'
}
$root = Join-Path $env:RUNNER_TEMP ('shiki-platform-wsl-' + [guid]::NewGuid().ToString('N'))
$public = Join-Path $root 'public'
$private = Join-Path $root 'private'
New-Item -ItemType Directory -Path $public, $private | Out-Null
$distro = 'ShikiCI-' + [guid]::NewGuid().ToString('N')
$storage = Join-Path $private 'distro'
$result = [ordered]@{ state = 'failed'; cleanup = 'pending'; runtime = 'WSL2'; distro = 'Ubuntu 24.04.5'; stage = 'prerequisites' }
$registered = $false
$summaryFile = Join-Path $public 'wsl.json'
$log = Join-Path $private 'driver.log'

function Invoke-Private([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments >> $log 2>&1
    if ($LASTEXITCODE -ne 0) { throw ('WSL stage failed: ' + $result.stage + '; exit ' + $LASTEXITCODE) }
}
function Fetch-Verified([string]$Url, [string]$Destination, [string]$Digest) {
    Invoke-WebRequest -Uri $Url -OutFile $Destination -MaximumRetryCount 3
    if ((Get-FileHash -Algorithm SHA256 -Path $Destination).Hash.ToLowerInvariant() -ne $Digest) {
        throw 'Fixed official download checksum mismatch'
    }
}
function Linux-Path([string]$WindowsPath) {
    $converted = & wsl.exe --distribution $distro --user root --exec wslpath -a $WindowsPath
    if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve WSL transfer path' }
    return (($converted -join "`n").Trim())
}
function Write-LinuxScript([string]$Name, [string]$Body) {
    $path = Join-Path $private $Name
    [IO.File]::WriteAllText($path, $Body.Replace("`r`n", "`n"), [Text.UTF8Encoding]::new($false))
    return (Linux-Path $path)
}

try {
    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) { throw 'WSL is not installed on this runner' }
    # No distro is installed as a side effect. If enabling WSL requires a reboot,
    # import below fails explicitly; an ordinary Ubuntu VM is never substituted.
    & wsl.exe --status >> $log 2>&1
    if ($LASTEXITCODE -ne 0) {
        Invoke-Private 'wsl.exe' @('--install', '--no-distribution', '--web-download')
    }
    $result.stage = 'fixed-downloads'
    $image = Join-Path $private 'ubuntu-24.04.5-wsl-amd64.wsl'
    Fetch-Verified 'https://releases.ubuntu.com/24.04.5/ubuntu-24.04.5-wsl-amd64.wsl' $image 'bb415d824822c4b878125729af451a5d18fb13d1cf5cbed9a7393ad64ac6039e'
    $goArchive = Join-Path $private 'go1.26.10.linux-amd64.tar.gz'
    Fetch-Verified 'https://go.dev/dl/go1.26.10.linux-amd64.tar.gz' $goArchive 'ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5'
    $result['rootfs_sha256'] = 'bb415d824822c4b878125729af451a5d18fb13d1cf5cbed9a7393ad64ac6039e'
    $result['go_sha256'] = 'ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5'
    $bundle = Join-Path $private 'source.bundle'
    Invoke-Private 'git' @('-C', $env:GITHUB_WORKSPACE, 'bundle', 'create', $bundle, 'HEAD')
    $result.stage = 'WSL2-import'
    New-Item -ItemType Directory -Path $storage | Out-Null
    # The GUID name is unique. This marker also covers a partially failed import.
    $registered = $true
    Invoke-Private 'wsl.exe' @('--import', $distro, $storage, $image, '--version', '2')
    $imagePath = Linux-Path $goArchive
    $bundlePath = Linux-Path $bundle
    $result.stage = 'Linux-bootstrap'
    $bootstrap = Write-LinuxScript 'bootstrap.sh' @'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git make gcc g++ libc6-dev python3 tmux unzip iproute2 openssl sudo systemd systemd-sysv dbus-user-session
useradd --create-home --shell /bin/bash shikici
printf 'shikici ALL=(ALL) NOPASSWD:ALL\n' > /etc/sudoers.d/shiki-ci
chmod 0440 /etc/sudoers.d/shiki-ci
printf '[boot]\nsystemd=true\n[user]\ndefault=shikici\n[interop]\nappendWindowsPath=false\n' > /etc/wsl.conf
tar -xzf "$1" -C /usr/local
runuser -u shikici -- git clone "$2" /home/shikici/source
runuser -u shikici -- git -C /home/shikici/source checkout --detach "$3"
mkdir -p /home/shikici/tmp
chown shikici:shikici /home/shikici/tmp
chmod 0700 /home/shikici/tmp
'@
    Invoke-Private 'wsl.exe' @('--distribution', $distro, '--user', 'root', '--exec', 'bash', $bootstrap, $imagePath, $bundlePath, $env:GITHUB_SHA)
    Invoke-Private 'wsl.exe' @('--terminate', $distro)
    $result.stage = 'WSL-systemd-restart'
    # Starting this exact distro reloads its boot configuration. Never wsl --shutdown.
    Invoke-Private 'wsl.exe' @('--distribution', $distro, '--user', 'root', '--exec', 'bash', '-c', 'for i in $(seq 1 60); do test "$(cat /proc/1/comm)" = systemd && exit 0; sleep 1; done; exit 1')
    $result.stage = 'product-and-maintenance'
    $acceptance = Write-LinuxScript 'acceptance.sh' @'
set -euo pipefail
export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export HOME=/home/shikici USER=shikici GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted RUNNER_ARCH=X64
export RUNNER_TEMP=/home/shikici/tmp SHIKI_PLATFORM_OS=linux SHIKI_PLATFORM_ARCH=amd64 SHIKI_PLATFORM_WSL=1
export GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS='-p=1 -mod=readonly -buildvcs=false'
cd /home/shikici/source
finish() {
    result=$?
    trap - EXIT
    if [ "$result" = 0 ]; then export SHIKI_PLATFORM_JOB_STATUS=success; else export SHIKI_PLATFORM_JOB_STATUS=failure; fi
    python3 -B scripts/platform-ci.py finalize --evidence "$RUNNER_TEMP/shiki-platform" || result=1
    exit "$result"
}
trap finish EXIT
python3 -B scripts/platform-ci.py preflight --evidence "$RUNNER_TEMP/shiki-platform"
make tools generate-proto
git diff --exit-code -- pkg/protocol
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 -scan=package ./...
go -C test/diagnostics/terminalstate/analyzer run golang.org/x/vuln/cmd/govulncheck@v1.1.4 -scan=package ./...
python3 -B scripts/platform-ci.py linux-pipeline --evidence "$RUNNER_TEMP/shiki-platform"
'@
    Invoke-Private 'wsl.exe' @('--distribution', $distro, '--user', 'shikici', '--exec', 'bash', $acceptance)
    $result.state = 'passed'
} catch {
    # Exception output is not copied to artifacts: network errors can include
    # machine paths. The fixed stage and failed outcome are sufficient publicly.
    Write-Host ('WSL acceptance failed at ' + $result.stage + '; details retained privately')
} finally {
    $cleanupFailed = $false
    if ($registered) {
        try {
            $destination = Linux-Path $public
            # Explicit filenames only. Never copy the Linux private/ tree.
            $export = Write-LinuxScript 'export.sh' @'
set -euo pipefail
source=/home/shikici/tmp/shiki-platform/public
for name in platform.json source.json stages.json result.json product-tests.json e2e-tests.json release-tests.json release-trust-tests.json systemd.json; do
    if [ -f "$source/$name" ] && [ ! -L "$source/$name" ]; then cp -- "$source/$name" "$1/$name"; fi
done
'@
            Invoke-Private 'wsl.exe' @('--distribution', $distro, '--user', 'root', '--exec', 'bash', $export, $destination)
        } catch { $result.state = 'failed' }
        & wsl.exe --terminate $distro >> $log 2>&1
        & wsl.exe --unregister $distro >> $log 2>&1
        # A failed import may never have registered. Verify exact-name absence.
        $names = & wsl.exe --list --quiet 2>> $log
        if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
        $normalized = ($names -join "`n").Replace([string][char]0, '') -split "`n"
        if (@($normalized | ForEach-Object { $_.Trim() }) -contains $distro) { $cleanupFailed = $true }
    }
    if (-not $cleanupFailed) {
        try { Remove-Item -LiteralPath $private -Recurse -Force } catch { $cleanupFailed = $true }
    }
    $result.cleanup = $(if ($cleanupFailed) { 'failed' } else { 'passed' })
    if ($cleanupFailed) { $result.state = 'failed' }
    $result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $summaryFile -Encoding utf8
    # Fixed output name; this path contains only a generated GUID, never user input.
    ('evidence=' + $public) >> $env:GITHUB_OUTPUT
}
if ($result.state -ne 'passed') { exit 1 }
