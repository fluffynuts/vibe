# Checks, with plain dotnet, what vibe's NuGet design (docs/plan-nuget-feeds.md)
# relies on, for a Windows machine. Needs the dotnet SDK on the PATH. Prints
# PASS, FAIL or SKIP for each check, and exits 1 if any failed. Touches nothing
# outside a temporary folder: a fake home, with a NuGet config of its own,
# stands in for yours.
#
#   .\check-nuget.ps1 [-PrivatePackage ID -PrivateVersion VERSION]
#
# With -PrivatePackage, one more check restores that package from the feeds in
# your own NuGet config, as vibe's fetch would. If `sbx` is installed, the last
# check says what path a read-only mount gets inside a sandbox.
#
# Written without a Windows machine to run it on: if a check misbehaves, say
# which, and what it printed.
param([string]$PrivatePackage = '', [string]$PrivateVersion = '')
$ErrorActionPreference = 'Stop'
if (-not (Get-Command dotnet -ErrorAction SilentlyContinue)) { Write-Error "dotnet isn't on the PATH"; exit 2 }
$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'; $env:DOTNET_NOLOGO = '1'; $env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE = '1'
$realAppData = $env:APPDATA; $realProfile = $env:USERPROFILE
$tfm = 'net' + (dotnet --version).Split('.')[0] + '.0'

$work = Join-Path ([IO.Path]::GetTempPath()) ("vibe-nuget-check-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$script:fails = 0
function Pass($name) { Write-Host "PASS  $name" }
function Fail($name, $why) { Write-Host ("FAIL  $name" + $(if ($why) { ": $why" } else { '' })); $script:fails++ }
function Skip($name, $why) { Write-Host ("SKIP  $name" + $(if ($why) { ": $why" } else { '' })) }
function Check($name, $ok, $why) { if ($ok) { Pass $name } else { Fail $name $why } }

# The fake host: its home and app data, a NuGet config naming one local feed.
$fakeHome = Join-Path $work 'home'
$fakeAppData = Join-Path $fakeHome 'AppData\Roaming'
$userConfig = Join-Path $fakeAppData 'NuGet\NuGet.Config'
$feed = Join-Path $work 'feed'
foreach ($d in @((Split-Path $userConfig), $feed, (Join-Path $work 'hostcache'), (Join-Path $work 'vibe\nuget-fetch'), (Join-Path $work 'workspace'))) {
  New-Item -ItemType Directory -Force -Path $d | Out-Null
}
$env:USERPROFILE = $fakeHome; $env:APPDATA = $fakeAppData
Set-Content -Path $userConfig -Value "<configuration><packageSources><clear/><add key=`"local`" value=`"$feed`"/></packageSources></configuration>"

# A package, Demo.Pkg 1.0.0, in the local feed.
$pack = Join-Path $work 'pack'
New-Item -ItemType Directory -Path $pack | Out-Null
Set-Content -Path (Join-Path $pack 'Demo.Pkg.csproj') -Value "<Project Sdk=`"Microsoft.NET.Sdk`"><PropertyGroup><TargetFramework>$tfm</TargetFramework><PackageId>Demo.Pkg</PackageId><Version>1.0.0</Version></PropertyGroup></Project>"
Set-Content -Path (Join-Path $pack 'C.cs') -Value 'class C {}'
Push-Location $pack
$env:NUGET_PACKAGES = Join-Path $work 'packcache'
$out = dotnet pack -o $feed 2>&1 | Out-String
Pop-Location
if ($LASTEXITCODE -ne 0) { Write-Host "couldn't make the test package:`n$out"; exit 2 }

function Fingerprint($dir) {
  $files = Get-ChildItem -Recurse -File $dir | Sort-Object FullName | ForEach-Object { "$($_.FullName.Substring($dir.Length)) $((Get-FileHash $_.FullName).Hash)" }
  [string]::Join("`n", $files)
}
function FetchProject($dir, $id = 'Demo.Pkg', $version = '1.0.0') {
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  Set-Content -Path (Join-Path $dir 'fetch.csproj') -Value "<Project Sdk=`"Microsoft.NET.Sdk`"><PropertyGroup><TargetFramework>$tfm</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include=`"$id`" Version=`"[$version]`" /></ItemGroup></Project>"
}
$noImports = @('-p:ImportDirectoryBuildProps=false', '-p:ImportDirectoryBuildTargets=false', '-p:ImportDirectoryPackagesProps=false')
function Fetch($dir, $cache) {
  Push-Location $dir
  $env:NUGET_PACKAGES = $cache
  try { return (dotnet restore @noImports 2>&1 | Out-String) } finally { Pop-Location }
}

try {
  Write-Host '== the sandbox side: the host cache as a fallback'
  $hostCache = Join-Path $work 'hostcache'
  FetchProject (Join-Path $work 'vibe\nuget-fetch\seed')
  Fetch (Join-Path $work 'vibe\nuget-fetch\seed') $hostCache | Out-Null
  Check "seeding the host's cache" (Test-Path (Join-Path $hostCache 'demo.pkg\1.0.0')) "Demo.Pkg isn't there"
  $before = Fingerprint $hostCache
  $app = Join-Path $work 'sandbox-app'
  FetchProject $app
  Push-Location $app
  $env:NUGET_PACKAGES = Join-Path $work 'sandboxcache'
  $env:NUGET_FALLBACK_PACKAGES = $hostCache
  $env:RestoreIgnoreFailedSources = 'true'
  $out = dotnet restore 2>&1 | Out-String
  $code = $LASTEXITCODE
  Remove-Item Env:NUGET_FALLBACK_PACKAGES, Env:RestoreIgnoreFailedSources
  Pop-Location
  Check "a restore with the host's cache as a fallback succeeds" ($code -eq 0) "the restore failed: $out"
  Check "the host's cache is left exactly as it was (a real mount is read-only; this checks nothing was written)" ($before -eq (Fingerprint $hostCache)) "something in the host's cache changed"

  Write-Host "== the host side: vibe's fetch"
  $folder = Join-Path $work 'vibe\nuget-fetch\one'
  FetchProject $folder
  Push-Location $folder
  $paths = dotnet nuget config paths 2>&1 | Out-String
  Pop-Location
  Check "the user's NuGet config is the one read, with no --configfile" ($paths -like "*$userConfig*") "the fake NuGet.Config isn't read. dotnet nuget config paths says: $paths"

  $cache2 = Join-Path $work 'hostcache2'; New-Item -ItemType Directory -Path $cache2 | Out-Null
  $marker = Join-Path $work 'marker'
  $plant = "<Project><Target Name=`"Pwn`" BeforeTargets=`"Restore;_GenerateRestoreGraph`"><Exec Command=`"echo ran > $marker`" /></Target></Project>"
  Set-Content -Path (Join-Path $work 'vibe\Directory.Build.targets') -Value $plant
  Set-Content -Path (Join-Path $work 'vibe\Directory.Build.props') -Value $plant
  Fetch $folder $cache2 | Out-Null
  Check "Directory.Build.props and .targets above the fetch folder aren't run" (-not (Test-Path $marker)) 'a Directory.Build file above the fetch folder ran'
  Check "the fetch puts the package in the cache it's given" (Test-Path (Join-Path $cache2 'demo.pkg\1.0.0')) "the package wasn't fetched"

  Set-Content -Path (Join-Path $work 'workspace\nuget.config') -Value '<configuration><packageSources><clear/><add key="local" value="https://evil.invalid/"/></packageSources></configuration>'
  $cache3 = Join-Path $work 'hostcache3'; New-Item -ItemType Directory -Path $cache3 | Out-Null
  $out = Fetch $folder $cache3
  Check "a nuget.config in the workspace (not above the fetch folder) is ignored" (Test-Path (Join-Path $cache3 'demo.pkg\1.0.0')) "a nuget.config in the workspace changed the fetch: $out"

  Set-Content -Path $userConfig -Value '<configuration><packageSources><clear/></packageSources></configuration>'
  $approved = Join-Path $work 'vibe\nuget-fetch\two'
  FetchProject $approved
  $cache4 = Join-Path $work 'hostcache4'; New-Item -ItemType Directory -Path $cache4 | Out-Null
  Fetch $approved $cache4 | Out-Null
  Check "with no feed in the user's config, a fetch finds nothing" (-not (Test-Path (Join-Path $cache4 'demo.pkg\1.0.0'))) 'the package came from nowhere'
  Set-Content -Path (Join-Path $approved 'nuget.config') -Value "<configuration><packageSources><add key=`"ws`" value=`"$feed`"/></packageSources></configuration>"
  Fetch $approved $cache4 | Out-Null
  Check "an approved workspace config, put beside the project, is used" (Test-Path (Join-Path $cache4 'demo.pkg\1.0.0')) "an approved config beside the project wasn't layered over the user's"

  Write-Host '== your own feeds'
  if ($PrivatePackage -and $PrivateVersion) {
    $env:APPDATA = $realAppData; $env:USERPROFILE = $realProfile
    $dir = Join-Path $work 'private-fetch'; $cache = Join-Path $work 'private-cache'
    FetchProject $dir $PrivatePackage $PrivateVersion
    New-Item -ItemType Directory -Path $cache | Out-Null
    $out = Fetch $dir $cache
    Check "$PrivatePackage $PrivateVersion is fetched with your own NuGet config" (Test-Path (Join-Path $cache (Join-Path $PrivatePackage.ToLower() $PrivateVersion))) "not fetched. dotnet said: $out"
    $env:APPDATA = $fakeAppData; $env:USERPROFILE = $fakeHome
  } else {
    Skip 'a package from your own private feed' 'pass -PrivatePackage ID -PrivateVersion VERSION'
  }

  Write-Host '== the mount, in a sandbox'
  if (Get-Command sbx -ErrorAction SilentlyContinue) {
    $env:APPDATA = $realAppData; $env:USERPROFILE = $realProfile
    $mountDir = Join-Path $work 'mounted'; New-Item -ItemType Directory -Path $mountDir | Out-Null
    Set-Content -Path (Join-Path $mountDir 'hello.txt') -Value 'hello'
    $name = 'vibe-nuget-check-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    Write-Host "creating a sandbox '$name' with $mountDir mounted read-only (the first sandbox can take a while)"
    sbx create --name $name claude $work "$mountDir`:ro"
    if ($LASTEXITCODE -eq 0) {
      Write-Host "INFO  inside the sandbox, find where hello.txt is (the path a read-only mount gets from a Windows path):"
      sbx exec $name -- find / -name hello.txt -not -path '/proc/*' 2>$null
      Write-Host "INFO  and whether it can be written:"
      sbx exec $name -- sh -c 'for f in $(find / -name hello.txt -not -path "/proc/*" 2>/dev/null); do touch "$(dirname $f)/probe" 2>&1 && echo "WRITABLE: $f" || echo "read-only: $f"; done'
      sbx rm $name -f | Out-Null
    } else {
      Fail 'creating a sandbox with a read-only mount' 'sbx create failed; see its output above'
    }
  } else {
    Skip 'the mount path inside a sandbox' 'sbx is not installed here'
  }

  Write-Host '== by hand'
  Write-Host 'MANUAL  Rider: after a fetch like the above, restore in a project on this machine and open it in Rider: do its packages resolve?'
  Write-Host "MANUAL  Credential providers (NuGet plugins) aren't covered; if you rely on one, try the -PrivatePackage check, and tell us what happens."
} finally {
  $env:APPDATA = $realAppData; $env:USERPROFILE = $realProfile
  Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

Write-Host ''
if ($script:fails -eq 0) { Write-Host 'all checks passed' } else { Write-Host "$($script:fails) check(s) failed"; exit 1 }
