#!/usr/bin/env pwsh
# vibe — build, test and tidy the CLI, for machines without make.
# Mirrors the Makefile: ./make.ps1 [build|test|vet|check|clean|help]... (default: build)
# (dist isn't here: it needs make.sh, as the Makefile's dist does.)
#
# The binary resolves its bundle (defaults/, profiles/, config.yaml,
# settings.yaml) relative to its own location, so it is built into the repo
# root and must stay there. Put it on $PATH with a symlink, never a copy.
#
# GO and BINARY can be overridden from the environment, as with make.

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$Go = if ($env:GO) { $env:GO } else { 'go' }
$Binary = if ($env:BINARY) { $env:BINARY } elseif ($IsWindows -or $env:OS -eq 'Windows_NT') { 'vibe.exe' } else { 'vibe' }
$Pkg = './src/vibe'

function Invoke-Step([string]$Command, [string[]]$Arguments) {
    Write-Host "$Command $($Arguments -join ' ')"
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

# build only when a source is newer than the binary, as make would.
function Invoke-Build {
    if (Test-Path $Binary -PathType Leaf) {
        $built = (Get-Item $Binary).LastWriteTimeUtc
        $newer = Get-ChildItem -Recurse -File -Include '*.go', 'go.mod', 'go.sum', 'VERSION' |
            Where-Object { $_.FullName -notmatch '[\\/]\.git[\\/]' -and $_.LastWriteTimeUtc -gt $built } |
            Select-Object -First 1
        if (-not $newer) {
            Write-Host "'$Binary' is up to date."
            return
        }
    }
    # The build date goes into vibe --version; Go doesn't record it itself.
    $date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'")
    Invoke-Step $Go @('build', '-ldflags', "-X vibe.BuildDate=$date", '-o', $Binary, $Pkg)
}

function Invoke-Test { Invoke-Step $Go @('test', './...') }

function Invoke-Vet { Invoke-Step $Go @('vet', './...') }

function Invoke-Check {
    Invoke-Vet
    Invoke-Test
}

function Invoke-Clean {
    Write-Host "rm -f $Binary"
    Remove-Item -Force -ErrorAction SilentlyContinue $Binary
    Write-Host 'rm -rf dist'
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue 'dist'
}

# What ./make.ps1 can do, in order, with what help says of each (the
# Makefile's words) and the function that does it.
$Targets = [ordered]@{
    'build' = @('build the vibe binary', { Invoke-Build })
    'test'  = @('run tests', { Invoke-Test })
    'vet'   = @('run go vet', { Invoke-Vet })
    'check' = @('run vet and tests', { Invoke-Check })
    'clean' = @('remove vibe binary and dist/ folder', { Invoke-Clean })
    'help'  = @('this help', { Invoke-Help })
}

function Invoke-Help {
    $colour = -not $env:NO_COLOR -and -not [Console]::IsOutputRedirected
    foreach ($name in $Targets.Keys) {
        $label = $name.PadRight(20)
        if ($colour) { $label = "$([char]27)[36m$label$([char]27)[0m" }
        Write-Host "$label $($Targets[$name][0])"
    }
}

$requested = if ($args.Count -gt 0) { $args } else { @('build') }

foreach ($target in $requested) {
    if (-not $Targets.Contains($target)) {
        [Console]::Error.WriteLine("make.ps1: no such target '$target' ($($Targets.Keys -join ', '))")
        exit 2
    }
    & $Targets[$target][1]
}
