# Install or upgrade vibe on Windows, from its latest GitHub release:
#
#   irm https://raw.githubusercontent.com/fluffynuts/vibe/master/install.ps1 | iex
#
# Downloads the release zip for this machine into a temporary folder, checks
# it against the release's SHA256SUMS, unpacks it into another, and runs
# vibe --install from there. To pass options on to vibe --install:
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/fluffynuts/vibe/master/install.ps1))) --update-strategy 'merge,keep'
#
# Runs in Windows PowerShell 5.1 and PowerShell 7. It never calls exit: run
# through iex, that would close the user's own PowerShell window.
# Once vibe is installed, vibe --upgrade does the same job.

function Install-Vibe {
    param([Parameter(ValueFromRemainingArguments = $true)] [string[]] $InstallArgs)

    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest's progress bar makes it crawl in 5.1

    if ($PSVersionTable.PSVersion.Major -ge 6 -and -not $IsWindows) {
        throw 'install.ps1 is for Windows — on Linux or macOS, use install.sh (see the readme)'
    }
    # Windows PowerShell 5.1 may still default to TLS 1.0, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $releases = if ($env:VIBE_RELEASES) { $env:VIBE_RELEASES } else { 'https://github.com/fluffynuts/vibe/releases' }

    # The machine's own architecture, not this PowerShell's: a 32-bit or
    # emulated shell on a 64-bit or ARM machine still wants that machine's build.
    $machine = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($machine) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "no vibe release for a $machine machine (only x86-64 and ARM64)" }
    }
    $asset = "vibe-windows-$arch.zip"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("vibe-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "vibe-install: downloading $asset"
        $zip = Join-Path $tmp $asset
        $sums = Join-Path $tmp 'SHA256SUMS'
        Invoke-WebRequest -UseBasicParsing -Uri "$releases/latest/download/$asset" -OutFile $zip
        Invoke-WebRequest -UseBasicParsing -Uri "$releases/latest/download/SHA256SUMS" -OutFile $sums

        $want = $null
        foreach ($line in Get-Content $sums) {
            $fields = $line -split '\s+', 2
            if ($fields.Count -eq 2 -and $fields[1].TrimStart('*') -eq $asset) { $want = $fields[0] }
        }
        if (-not $want) { throw "SHA256SUMS has no entry for $asset" }
        $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash
        # (A release published between the two downloads above would make
        # them disagree too: running this again settles that.)
        if ($got -ne $want) { throw "$asset doesn't match its checksum (got $got, want $want) — not installing it" }
        Write-Host "vibe-install: checked $asset against SHA256SUMS"

        $unpacked = Join-Path $tmp 'unpacked'
        Expand-Archive -Path $zip -DestinationPath $unpacked
        $bundle = Get-ChildItem -Path $unpacked -Directory | Select-Object -First 1
        $exe = if ($bundle) { Join-Path $bundle.FullName 'vibe.exe' }
        if (-not $exe -or -not (Test-Path $exe)) { throw "the release zip doesn't hold vibe.exe" }

        Write-Host "vibe-install: running vibe --install from $($bundle.Name)"
        & $exe --install @InstallArgs
        if ($LASTEXITCODE -ne 0) { throw "vibe --install didn't finish cleanly (exit $LASTEXITCODE) — see above" }
    }
    finally {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp
    }
}

Install-Vibe @args
