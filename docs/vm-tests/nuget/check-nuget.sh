#!/usr/bin/env bash
# Checks, with plain dotnet, what vibe's NuGet design (docs/plan-nuget-feeds.md)
# relies on. For macOS and Linux machines, such as a macOS VM that can't run sbx.
# Needs the dotnet SDK on the PATH. Prints PASS, FAIL or SKIP for each check,
# and exits 1 if any failed. Touches nothing outside a temporary folder: a fake
# home, with a NuGet config of its own, stands in for yours.
#
#   check-nuget.sh [--private-package ID --private-version VERSION]
#
# With --private-package, one more check restores that package from the feeds in
# your own NuGet config, as vibe's fetch would.
set -u

private_id=""
private_version=""
while [ $# -gt 0 ]; do
  case "$1" in
    --private-package) private_id=$2; shift 2 ;;
    --private-version) private_version=$2; shift 2 ;;
    *) echo "usage: $0 [--private-package ID --private-version VERSION]" >&2; exit 2 ;;
  esac
done

command -v dotnet >/dev/null || { echo "dotnet isn't on the PATH" >&2; exit 2; }
export DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
real_home=$HOME
tfm="net$(dotnet --version | cut -d. -f1).0"

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
fails=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1${2:+: $2}"; fails=$((fails + 1)); }
skip() { echo "SKIP  $1${2:+: $2}"; }
check() { if [ "$2" = ok ]; then pass "$1"; else fail "$1" "$2"; fi; }

# The fake host: its home, a NuGet config naming one local feed, a package cache.
export HOME=$work/home
mkdir -p "$HOME/.nuget/NuGet" "$work/feed" "$work/hostcache" "$work/vibe/nuget-fetch" "$work/workspace"
cat > "$HOME/.nuget/NuGet/NuGet.Config" <<XML
<configuration><packageSources><clear/><add key="local" value="$work/feed"/></packageSources></configuration>
XML

# A package, Demo.Pkg 1.0.0, in the local feed.
mkdir "$work/pack"
cat > "$work/pack/Demo.Pkg.csproj" <<XML
<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>$tfm</TargetFramework><PackageId>Demo.Pkg</PackageId><Version>1.0.0</Version></PropertyGroup></Project>
XML
echo 'class C {}' > "$work/pack/C.cs"
if ! (cd "$work/pack" && NUGET_PACKAGES=$work/packcache dotnet pack -o "$work/feed" >"$work/pack.log" 2>&1); then
  echo "couldn't make the test package:"; cat "$work/pack.log"; exit 2
fi

fingerprint() { (cd "$1" && find . -type f -exec shasum {} + 2>/dev/null | sort | shasum | cut -d' ' -f1); }

fetch_project() { # folder: a throwaway project that asks for Demo.Pkg
  mkdir -p "$1"
  cat > "$1/fetch.csproj" <<XML
<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>$tfm</TargetFramework></PropertyGroup>
<ItemGroup><PackageReference Include="Demo.Pkg" Version="[1.0.0]" /></ItemGroup></Project>
XML
}
NOIMPORTS=(-p:ImportDirectoryBuildProps=false -p:ImportDirectoryBuildTargets=false -p:ImportDirectoryPackagesProps=false)
fetch() { # folder cache: vibe's fetch of that project
  (cd "$1" && NUGET_PACKAGES=$2 dotnet restore "${NOIMPORTS[@]}" 2>&1)
}

echo "== the sandbox side: the host's cache read-only, as a fallback"
fetch_project "$work/vibe/nuget-fetch/seed"
fetch "$work/vibe/nuget-fetch/seed" "$work/hostcache" >/dev/null
[ -d "$work/hostcache/demo.pkg/1.0.0" ] || fail "seeding the host's cache" "Demo.Pkg isn't there"
before=$(fingerprint "$work/hostcache")
chmod -R a-w "$work/hostcache"
mkdir -p "$work/sandbox-app"
fetch_project "$work/sandbox-app"
out=$(cd "$work/sandbox-app" && NUGET_FALLBACK_PACKAGES=$work/hostcache NUGET_PACKAGES=$work/sandboxcache RestoreIgnoreFailedSources=true dotnet restore 2>&1)
if [ $? -eq 0 ]; then r=ok; else r="the restore failed: $out"; fi
check "a restore with the host's cache as a read-only fallback succeeds" "$r"
chmod -R u+w "$work/hostcache"
[ "$before" = "$(fingerprint "$work/hostcache")" ] && r=ok || r="something in the host's cache changed"
check "the host's cache is left exactly as it was" "$r"

echo "== the host side: vibe's fetch"
folder=$work/vibe/nuget-fetch/one
fetch_project "$folder"
paths=$(cd "$folder" && dotnet nuget config paths 2>&1)
case "$paths" in
  *"$HOME/.nuget/NuGet/NuGet.Config"*) r=ok ;;
  *) r="the fake home's NuGet.Config isn't read. dotnet nuget config paths says: $paths" ;;
esac
check "the user's NuGet config is the one read, with no --configfile" "$r"

rm -rf "$work/hostcache2"; mkdir "$work/hostcache2"
marker=$work/marker
plant='<Project><Target Name="Pwn" BeforeTargets="Restore;_GenerateRestoreGraph"><Exec Command="touch '"$marker"'" /></Target></Project>'
echo "$plant" > "$work/vibe/Directory.Build.targets"
echo "$plant" > "$work/vibe/Directory.Build.props"
fetch "$folder" "$work/hostcache2" >/dev/null
[ -e "$marker" ] && r="a Directory.Build file above the fetch folder ran" || r=ok
check "Directory.Build.props and .targets above the fetch folder aren't run" "$r"
[ -d "$work/hostcache2/demo.pkg/1.0.0" ] && r=ok || r="the package wasn't fetched"
check "the fetch puts the package in the cache it's given" "$r"

echo '<configuration><packageSources><clear/><add key="local" value="https://evil.invalid/"/></packageSources></configuration>' > "$work/workspace/nuget.config"
rm -rf "$work/hostcache3"; mkdir "$work/hostcache3"
out=$(fetch "$folder" "$work/hostcache3")
[ -d "$work/hostcache3/demo.pkg/1.0.0" ] && r=ok || r="a nuget.config in the workspace changed the fetch: $out"
check "a nuget.config in the workspace (not above the fetch folder) is ignored" "$r"

echo '<configuration><packageSources><clear/></packageSources></configuration>' > "$HOME/.nuget/NuGet/NuGet.Config"
approved=$work/vibe/nuget-fetch/two
fetch_project "$approved"
rm -rf "$work/hostcache4"; mkdir "$work/hostcache4"
fetch "$approved" "$work/hostcache4" >/dev/null
[ -d "$work/hostcache4/demo.pkg/1.0.0" ] && r="the package came from nowhere" || r=ok
check "with no feed in the user's config, a fetch finds nothing" "$r"
cat > "$approved/nuget.config" <<XML
<configuration><packageSources><add key="ws" value="$work/feed"/></packageSources></configuration>
XML
fetch "$approved" "$work/hostcache4" >/dev/null
[ -d "$work/hostcache4/demo.pkg/1.0.0" ] && r=ok || r="an approved config beside the project wasn't layered over the user's"
check "an approved workspace config, put beside the project, is used" "$r"

echo "== your own feeds"
if [ -n "$private_id" ] && [ -n "$private_version" ]; then
  HOME=$real_home
  mkdir -p "$work/private-fetch" "$work/private-cache"
  cat > "$work/private-fetch/fetch.csproj" <<XML
<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>$tfm</TargetFramework></PropertyGroup>
<ItemGroup><PackageReference Include="$private_id" Version="[$private_version]" /></ItemGroup></Project>
XML
  out=$(cd "$work/private-fetch" && NUGET_PACKAGES=$work/private-cache dotnet restore "${NOIMPORTS[@]}" 2>&1)
  lower=$(printf '%s' "$private_id" | tr '[:upper:]' '[:lower:]')
  [ -d "$work/private-cache/$lower/$private_version" ] && r=ok || r="not fetched. dotnet said: $out"
  check "$private_id $private_version is fetched with your own NuGet config" "$r"
else
  skip "a package from your own private feed" "pass --private-package ID --private-version VERSION"
fi

echo "== by hand"
echo "MANUAL  Rider: after a fetch like the above, restore in a project on this machine and open it in Rider: do its packages resolve?"
echo "MANUAL  Credential providers (NuGet plugins) aren't covered; if you rely on one, try the --private-package check, and tell us what happens."

echo
if [ "$fails" -eq 0 ]; then echo "all checks passed"; else echo "$fails check(s) failed"; exit 1; fi
