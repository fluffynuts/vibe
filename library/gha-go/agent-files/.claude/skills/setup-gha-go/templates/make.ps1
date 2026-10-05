#!/usr/bin/env pwsh
# {{NAME}} — build, test and tidy the CLI, for machines without make.
# Mirrors make.sh: ./make.ps1 [build|test|vet|check|clean|dist]... (default: build)
#
# GO and BINARY can be overridden from the environment, as with make.
# dist needs a zip(1) equivalent: it uses Compress-Archive, so unlike
# make.sh's zips the binary does not keep an executable bit; CI uses make.sh.

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$Name = '{{NAME}}'
$Go = if ($env:GO) { $env:GO } else { 'go' }
$Binary = if ($env:BINARY) { $env:BINARY } elseif ($IsWindows -or $env:OS -eq 'Windows_NT') { "$Name.exe" } else { $Name }
$Pkg = '{{PKG}}'
# Extra files shipped beside the binary in each zip; they must exist.
$Bundle = @('VERSION')

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
    Invoke-Step $Go @('build', '-o', $Binary, $Pkg)
}

function Invoke-Test { Invoke-Step $Go @('test', './...') }

function Invoke-Vet { Invoke-Step $Go @('vet', './...') }

function Invoke-Check {
    Invoke-Vet
    Invoke-Test
}

function Invoke-Dist {
    $goos = if ($env:GOOS) { $env:GOOS } else { (& $Go env GOOS).Trim() }
    $goarch = if ($env:GOARCH) { $env:GOARCH } else { (& $Go env GOARCH).Trim() }
    $osName = if ($goos -eq 'darwin') { 'macos' } else { $goos }
    $version = (Get-Content VERSION -Raw).Trim()
    $full = if ($env:BUILD) { "$version.$($env:BUILD)" } else { $version }
    $dist = "$Name-$full-$osName-$goarch"
    $exe = if ($goos -eq 'windows') { "$Name.exe" } else { $Name }
    $stage = Join-Path 'dist' $dist
    $zip = Join-Path 'dist' "$dist.zip"

    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $stage, $zip
    New-Item -ItemType Directory -Force $stage | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    Invoke-Step $Go @('build', '-trimpath', '-ldflags', "-X main.Version=$version -X main.Build=$($env:BUILD)", '-o', (Join-Path $stage $exe), $Pkg)
    Copy-Item -Recurse $Bundle $stage
    Compress-Archive -Path $stage -DestinationPath $zip
    Remove-Item -Recurse -Force $stage
    Write-Host $zip
}

function Invoke-Clean {
    Write-Host "rm -f $Binary"
    Remove-Item -Force -ErrorAction SilentlyContinue $Binary
    Write-Host 'rm -rf dist'
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue 'dist'
}

$targets = if ($args.Count -gt 0) { $args } else { @('build') }

foreach ($target in $targets) {
    switch ($target) {
        'build' { Invoke-Build }
        'test' { Invoke-Test }
        'vet' { Invoke-Vet }
        'check' { Invoke-Check }
        'clean' { Invoke-Clean }
        'dist' { Invoke-Dist }
        default {
            [Console]::Error.WriteLine("make.ps1: no such target '$target' (build, test, vet, check, clean, dist)")
            exit 2
        }
    }
}
