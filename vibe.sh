#!/usr/bin/env bash
# this is the original incarnation of the app, written in bash. It should
# be used as a reference only
#
# vibe — open (creating if needed) a Claude Code sandbox for a project folder.
#
#   vibe                    # use $PWD
#   vibe /path/to/code      # use an explicit folder
#   vibe -n custom-name .   # override the derived sandbox name
#   vibe --stop             # stop the sandbox for $PWD
#   vibe --stop /path       # stop the sandbox for an explicit folder
#   vibe --ssh              # ssh into the sandbox for $PWD
#   vibe --ssh /path        # ssh into the sandbox for an explicit folder
#   vibe --re-init          # remove the sandbox, rewrite the kit, start fresh
#   vibe --init             # (re)write the bundled kit spec, then exit
#   vibe --init -f          # ...without prompting to replace an existing one
#
# --stop, --ssh and --re-init resolve the sandbox name exactly as a normal run
# would, so `vibe --stop && vibe` restarts whatever you are working on, and
# `vibe --re-init` rebuilds it from the current bundled spec.
#
# --ssh requires `sbx setup ssh` to have been run once on this machine.
#
# --re-init confirms before removing and before replacing the spec; -f skips
# both prompts.
#
# The kit spec is written to $SBX_CC_KIT (default ~/.sbx/kits/diffity) on first
# use, and never overwritten afterwards except via --init or --re-init.
#
# Creation-only settings (--memory, --publish, --kit) are applied by `sbx create`.
# Once a sandbox exists this script only attaches; remove it with `sbx rm <name>`
# or --re-init if you need those settings changed.

set -euo pipefail


readonly KIT="${SBX_CC_KIT:-${HOME}/.sbx/kits/diffity}"
readonly MEMORY="${SBX_CC_MEMORY:-12g}"
readonly BASE_PORT="${SBX_CC_PORT:-5391}"
readonly AGENT="${SBX_CC_AGENT:-claude}"
readonly NUGET_LINKER="/home/agent/.local/bin/link-nuget"
readonly RABBIT_STARTER="/home/agent/.local/bin/start-rabbit"
readonly SERVICE_STARTER="/home/agent/.local/bin/start-services"
# Host ~/.nuget is mounted into the sandbox so the package cache, NuGet.Config
# and private-feed credentials are shared. Set SBX_CC_NUGET="" to disable.
readonly NUGET_DIR="${SBX_CC_NUGET-${HOME}/.nuget}"
# Agent memories live at /home/agent/.claude/projects, which is claude-specific.
# A host directory is mounted in so backup/restore is a plain cp inside the
# sandbox rather than streaming a tar through sbx exec's stdio.
readonly MEMORY_ROOT="${SBX_CC_MEMORY_ROOT:-${HOME}/.vibe/memories}"
readonly AGENT_MEMORY_PATH="/home/agent/.claude/projects"

die() { printf "vibe: %s\n" "$*" >&2; exit 1; }
note() { printf "vibe: %s\n" "$*" >&2; }

usage() {
  sed -n "3,30p" "$0" | sed "s/^# \{0,1\}//"
}

sbx version >/dev/null 2>&1 </dev/null \
  || die "sbx needs attention — run 'sbx version' directly (it may be waiting for input after an update)"

# --init only touches local config, so it doesn't need sbx present.
case " $* " in
  *" --init "*) ;;
  *)
    command -v sbx >/dev/null 2>&1 || die "\
'sbx' is not on PATH — this script needs Docker Sandboxes.
Install it from https://github.com/docker/sbx-releases/releases,
then make sure its bin directory is on PATH (e.g. \${HOME}/.docker/sbx/bin)."
    ;;
esac

# Emit the bundled kit spec on stdout.
kit_spec() {
  cat <<'SPEC'
schemaVersion: "2"
kind: mixin
name: diffity-dotnet
displayName: Diffity review tooling + .NET SDK
description: GitHub-style diff viewer, agent review skills, and the .NET 8 SDK
sourceURL: https://github.com/nilbuild/diffity
requires:
  agent: claude
permissions:
  network:
    allow:
      - registry.npmjs.org
      - github.com
      - "*.pkg.github.com"
      - codeload.github.com
      - objects.githubusercontent.com
      # skills CLI
      - add-skill.vercel.sh
      # apt
      - archive.ubuntu.com
      - security.ubuntu.com
      - ports.ubuntu.com
      - "*.ubuntu.com"
      - packages.microsoft.com
      # Elasticsearch
      - artifacts.elastic.co
      # NuGet restore
      - api.nuget.org
      - "*.nuget.org"
      # dotnet-install.sh — aka.ms is where it redirects from
      - dot.net
      - aka.ms
      - builds.dotnet.microsoft.com
      - dotnetcli.azureedge.net
      - dotnetcli.blob.core.windows.net
ports:
  - container: 5391
    protocol: tcp
    name: diffity
environment:
  variables:
    DIFFITY_HOST: "localhost"
    DOTNET_CLI_TELEMETRY_OPTOUT: "1"
    DOTNET_NOLOGO: "1"
setup:
  install:
    # Every step below appends to /tmp/sbx-setup.log and exits 0 no matter what,
    # so a broken toolchain never blocks the sandbox from booting.
    # Investigate with:  sbx exec <name> -- cat /tmp/sbx-setup.log
    - command: 'echo "=== setup started $(date -Is) ===" >>/tmp/sbx-setup.log 2>&1; chmod 666 /tmp/sbx-setup.log 2>/dev/null; true'
      user: "0"
      description: Start the setup log
    - command: 'i=0; ok=0; while [ $i -lt 5 ]; do i=$((i+1)); echo "--- apt-get update attempt $i $(date -Is)" >>/tmp/sbx-setup.log; if sudo apt-get -o DPkg::Lock::Timeout=300 update >>/tmp/sbx-setup.log 2>&1; then ok=1; echo "apt-get update OK on attempt $i" >>/tmp/sbx-setup.log; break; fi; echo "apt-get update FAILED on attempt $i, sleeping 10s" >>/tmp/sbx-setup.log; sleep 10; done; [ $ok -eq 1 ] || echo "WARN: apt-get update never succeeded — see log above" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Refresh apt indexes (waits for dpkg lock, retries, non-fatal)
    - command: 'echo "--- installing dotnet-sdk-10.0 $(date -Is)" >>/tmp/sbx-setup.log; sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y dotnet-sdk-10.0 </dev/null >>/tmp/sbx-setup.log 2>&1 || echo "WARN: dotnet-sdk-10.0 install failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Install the .NET 10 SDK from the distro (non-fatal)
    - command: 'echo "--- installing mysql,redis,rabbitmq,file $(date -Is)" >>/tmp/sbx-setup.log; sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y mysql-server redis-server rabbitmq-server file </dev/null >>/tmp/sbx-setup.log 2>&1 || echo "WARN: service dependencies install failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Install mysql-server, redis-server and rabbitmq-server (non-fatal)
    - command: 'echo "--- installing .NET 8 ASP.NET Core runtime $(date -Is)" >>/tmp/sbx-setup.log; if command -v dotnet >/dev/null 2>&1; then root=$(dirname "$(readlink -f "$(command -v dotnet)")"); echo "DOTNET_ROOT=$root" >>/tmp/sbx-setup.log; curl -fsSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh >>/tmp/sbx-setup.log 2>&1 && sudo bash /tmp/dotnet-install.sh --runtime aspnetcore --channel 8.0 --install-dir "$root" --no-path </dev/null >>/tmp/sbx-setup.log 2>&1 || echo "WARN: .NET 8 runtime install failed" >>/tmp/sbx-setup.log; else echo "WARN: no dotnet on PATH, skipping .NET 8 runtime" >>/tmp/sbx-setup.log; fi; true'
      user: "0"
      description: Add the .NET 8 ASP.NET Core runtime so net8.0 tests can run (non-fatal)
    - command: 'echo "--- dotnet inventory $(date -Is)" >>/tmp/sbx-setup.log; { echo "SDKs:"; dotnet --list-sdks; echo "Runtimes:"; dotnet --list-runtimes; } >>/tmp/sbx-setup.log 2>&1 || echo "WARN: dotnet not usable" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Record which SDKs and runtimes ended up installed
    - command: 'echo "--- npm install -g diffity $(date -Is)" >>/tmp/sbx-setup.log; npm install -g diffity >>/tmp/sbx-setup.log 2>&1 || echo "WARN: diffity install failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Install diffity globally (non-fatal)
    - command: "install -d -o agent -g agent -m 0755 /home/agent/.local/bin"
      user: "0"
      description: Ensure the launcher's parent directory exists
    - command: 'echo "--- skills add $(date -Is)" >>/tmp/sbx-setup.log; cd /home/agent && npx -y skills add nilbuild/diffity -- -y </dev/null >>/tmp/sbx-setup.log 2>&1 || echo "WARN: skills add failed" >>/tmp/sbx-setup.log; true'
      user: "1000"
      description: Install diffity agent skills for the agent user (non-interactive, non-fatal)
    - command: 'mkdir -p /home/agent/.claude/skills; for d in /home/agent/.agents/skills /home/agent/workspace/.agents/skills /home/agent/workspace/.claude/skills; do [ -d "$d" ] && cp -a "$d"/. /home/agent/.claude/skills/; done; { echo "--- skills present:"; ls /home/agent/.claude/skills; } >>/tmp/sbx-setup.log 2>&1; ls /home/agent/.claude/skills; true'
      user: "1000"
      description: Consolidate skills into ~/.claude/skills wherever the installer put them
    - command: 'echo "--- elasticsearch repo $(date -Is)" >>/tmp/sbx-setup.log; curl -fsSL https://artifacts.elastic.co/GPG-KEY-elasticsearch 2>>/tmp/sbx-setup.log | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/elasticsearch-keyring.gpg 2>>/tmp/sbx-setup.log && echo "deb [signed-by=/usr/share/keyrings/elasticsearch-keyring.gpg] https://artifacts.elastic.co/packages/9.x/apt stable main" | sudo tee /etc/apt/sources.list.d/elastic-9.x.list >/dev/null && sudo apt-get -o DPkg::Lock::Timeout=300 update >>/tmp/sbx-setup.log 2>&1 || echo "WARN: elastic repo setup failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Add the Elastic apt repository
    - command: 'echo "--- installing elasticsearch $(date -Is)" >>/tmp/sbx-setup.log; sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y elasticsearch </dev/null >>/tmp/sbx-setup.log 2>&1 || echo "WARN: elasticsearch install failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Install Elasticsearch (no Ubuntu package exists)
    - command: 'printf "%s\n" "cluster.name: yumbi-sandbox" "discovery.type: single-node" "network.host: 127.0.0.1" "http.port: 9200" "path.data: /var/lib/elasticsearch" "path.logs: /var/log/elasticsearch" "xpack.security.enabled: false" "xpack.security.enrollment.enabled: false" "xpack.ml.enabled: false" | sudo tee /etc/elasticsearch/elasticsearch.yml >/dev/null && printf "%s\n" "-Xms512m" "-Xmx512m" | sudo tee /etc/elasticsearch/jvm.options.d/heap.options >/dev/null && sudo install -d -o elasticsearch -g elasticsearch -m 0750 /var/lib/elasticsearch /var/log/elasticsearch && sudo install -d -o elasticsearch -g elasticsearch -m 0755 /usr/share/elasticsearch/logs /usr/share/elasticsearch/tmp && echo "--- wrote elasticsearch config" >>/tmp/sbx-setup.log || echo "WARN: elasticsearch config failed" >>/tmp/sbx-setup.log; true'
      user: "0"
      description: Single-node, security-disabled config with a pinned heap
    - command: 'echo "=== setup finished $(date -Is) ===" >>/tmp/sbx-setup.log; echo "--- setup warnings ---"; grep -E "^(WARN|ERROR)" /tmp/sbx-setup.log || echo "none"; echo "--- full log: sbx exec <name> -- cat /tmp/sbx-setup.log"; true'
      user: "0"
      description: Summarise any warnings from setup
  files:
    - path: /home/agent/.claude/settings.json
      mode: "0644"
      onlyIfMissing: true
      description: Deny rules for secrets — these still apply in bypass mode
      content: |
        {
          "permissions": {
            "deny": [
              "Read(./.env)",
              "Read(./.env.*)",
              "Read(**/*.pem)",
              "Read(**/id_rsa*)"
            ]
          }
        }
    - path: /home/agent/.local/bin/start-rabbit
      mode: "0755"
      description: Start RabbitMQ with a stable node name, then ensure the rabbit user
      content: |
        #!/bin/sh
        # No braced shell expansions: sbx validates every brace-form
        # substitution in file content against its own placeholder list.
        exec >>/tmp/rabbitmq.log 2>&1
        echo "=== start-rabbit at $(date -Is) ==="
        sleep 5

        NODE=rabbit@localhost
        CTL="sudo -u rabbitmq env RABBITMQ_NODENAME=$NODE rabbitmqctl"

        # 1628 is 5672 in hex, 0A is LISTEN.
        if awk '$2 ~ /:1628$/ && $4 == "0A" { f = 1 } END { exit f ? 0 : 1 }' /proc/net/tcp; then
          echo "broker already listening on 5672"
        else
          # The mnesia tree must be owned by rabbitmq or ra_log_ets crashes
          # with eacces on quorum/.../names.dets and boot aborts.
          # sudo because this runs as root from setup.startup but as the agent
          # user when invoked via sbx exec from vibe's nudge.
          sudo install -d -o rabbitmq -g rabbitmq -m 750 /var/lib/rabbitmq/mnesia \
            || echo "WARN: could not prepare /var/lib/rabbitmq/mnesia"
          echo "starting broker as $NODE"
          sudo -u rabbitmq env RABBITMQ_NODENAME=$NODE RABBITMQ_LOG_BASE=/tmp \
            rabbitmq-server -detached
        fi

        # rabbitmqctl needs a live node; wait rather than assume.
        i=0
        ready=0
        while [ $i -lt 60 ]; do
          if $CTL await_startup --timeout 5 >/dev/null 2>&1; then
            ready=1
            break
          fi
          i=$((i + 1))
          sleep 1
        done
        if [ $ready -eq 0 ]; then
          echo "ERROR: broker not ready after $i seconds"
          echo "check /tmp/rabbit@localhost.log for the boot failure"
          exit 1
        fi
        echo "broker ready after $i seconds"

        # Idempotent: startup commands replay on every container restart.
        if $CTL list_users 2>/dev/null | awk 'NR > 1 && $1 == "rabbit" { f = 1 } END { exit f ? 0 : 1 }'; then
          echo "user 'rabbit' already exists"
        else
          echo "creating user 'rabbit'"
          $CTL add_user rabbit rabbit || echo "WARN: add_user failed"
        fi
        $CTL set_permissions -p / rabbit '.*' '.*' '.*' || echo "WARN: set_permissions failed"
        $CTL set_user_tags rabbit administrator || echo "WARN: set_user_tags failed"
        echo "rabbit user provisioned"
    - path: /home/agent/.local/bin/link-nuget
      mode: "0755"
      description: Point ~/.nuget at the mounted host NuGet dir, if one was passed
      content: |
        #!/bin/sh
        # SBX_HOST_NUGET is set by vibe when it mounts the host ~/.nuget.
        # No braced shell expansions here: sbx validates every brace-form
        # substitution in file content against its own placeholder list.
        exec >>/tmp/link-nuget.log 2>&1
        echo "=== link-nuget at $(date -Is) ==="
        if [ -z "$SBX_HOST_NUGET" ]; then
          echo "SBX_HOST_NUGET unset — nothing to link"
          exit 0
        fi
        if [ ! -d "$SBX_HOST_NUGET" ]; then
          echo "ERROR: $SBX_HOST_NUGET is not mounted"
          exit 1
        fi
        LINK=/home/agent/.nuget
        if [ -L "$LINK" ]; then
          ln -sfn "$SBX_HOST_NUGET" "$LINK"
        elif [ -e "$LINK" ]; then
          echo "WARN: $LINK exists and is not a symlink; moving aside"
          mv "$LINK" "$LINK.local" && ln -s "$SBX_HOST_NUGET" "$LINK"
        else
          ln -s "$SBX_HOST_NUGET" "$LINK"
        fi
        echo "linked $LINK -> $(readlink "$LINK")"
        ls "$LINK" 2>/dev/null | head

        # Restore bakes absolute package paths into obj/project.assets.json.
        # Left to itself NuGet resolves via HOME, giving /home/agent/.nuget,
        # which does not exist on the host — so Rider cannot resolve a single
        # package after a container build. Pinning both sides to the host path
        # keeps the generated files identical. Written to the persistent env
        # file rather than passed at create time so it cannot go missing on a
        # sandbox created before this change.
        PERSIST=/etc/sandbox-persistent.sh
        WANT="export NUGET_PACKAGES=$SBX_HOST_NUGET/packages"
        if [ -f "$PERSIST" ] && grep -qxF "$WANT" "$PERSIST"; then
          echo "NUGET_PACKAGES already persisted"
        else
          sudo sh -c "sed -i '/^export NUGET_PACKAGES=/d' '$PERSIST' 2>/dev/null; \
            echo '$WANT' >> '$PERSIST'" \
            && echo "persisted: $WANT" \
            || echo "WARN: could not write $PERSIST"
        fi
    - path: /home/agent/.local/bin/start-elasticsearch
      mode: "0755"
      description: Start Elasticsearch on 9200 and wait until it answers
      content: |
        #!/bin/sh
        # No braced shell expansions: sbx validates every brace-form
        # substitution in file content against its own placeholder list.
        exec >>/tmp/elasticsearch.log 2>&1
        echo "=== start-elasticsearch at $(date -Is) ==="

        # 23F0 is 9200 in hex, 0A is LISTEN.
        if awk '$2 ~ /:23F0$/ && $4 == "0A" { f = 1 } END { exit f ? 0 : 1 }' /proc/net/tcp; then
          echo "already listening on 9200 — nothing to do"
          exit 0
        fi

        if [ ! -x /usr/share/elasticsearch/bin/elasticsearch ]; then
          echo "ERROR: elasticsearch is not installed"
          exit 1
        fi

        # ES refuses to start below this; the microVM has its own kernel so
        # the sysctl is ours to set.
        sudo sysctl -w vm.max_map_count=262144 || echo "WARN: could not set vm.max_map_count"

        echo "Recreating elasticsearch keystore"
        sudo rm -f /etc/elasticsearch/elasticsearch.keystore
        sudo /usr/share/elasticsearch/bin/elasticsearch-keystore create

        echo "starting elasticsearch"
        sudo -u elasticsearch env ES_PATH_CONF=/etc/elasticsearch \
          ES_TMPDIR=/usr/share/elasticsearch/tmp \
          setsid /usr/share/elasticsearch/bin/elasticsearch -d -p /tmp/elasticsearch.pid

        # The wrapper in Yumbi probes for HTTP 200, so wait for that, not just
        # for the port to open.
        i=0
        while [ $i -lt 120 ]; do
          code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9200 2>/dev/null)
          if [ "$code" = "200" ]; then
            echo "elasticsearch ready after $i seconds"
            exit 0
          fi
          i=$((i + 1))
          sleep 1
        done
        echo "ERROR: elasticsearch not answering 200 after $i seconds (last code: $code)"
        echo "check /var/log/elasticsearch/yumbi-sandbox.log"
        exit 1
    - path: /home/agent/.local/bin/start-services
      mode: "0755"
      description: Run every service starter in order
      content: |
        #!/bin/sh
        exec >>/tmp/start-services.log 2>&1
        echo "=== start-services at $(date -Is) ==="
        for s in link-nuget start-rabbit start-elasticsearch; do
          echo "--- $s"
          /home/agent/.local/bin/$s || echo "WARN: $s exited $?"
        done
        echo "=== done at $(date -Is) ==="
  startup:
    - command: ["/home/agent/.local/bin/start-services"]
      user: "0"
      background: true
      description: Link nuget, start RabbitMQ & Elasticsearch

agentInstructions:
  content: |
    ## Toolchain

    The .NET SDK is installed — run `dotnet --list-sdks` to see which versions.
    `dotnet build` and `dotnet test -c IsolatedTesting` work against the workspace.
    It is vital to run tests with the build configuration IsolatedTesting, otherwise
    tests WILL fail in this environment.

    NuGet restore reaches api.nuget.org only — if a restore fails on a private feed,
    say so rather than trying to work around it.

    ## Services

    RabbitMQ runs on 5672 as node `rabbit@localhost`, with user `rabbit`
    (password `rabbit`) holding full permissions on `/`. Use
    `sudo -u rabbitmq env RABBITMQ_NODENAME=rabbit@localhost rabbitmqctl ...`
    for any admin commands — without the node name it will not find the broker.
    Startup log: `/tmp/rabbitmq.log`; broker log: `/tmp/rabbit@localhost.log`.

    Elasticsearch runs on http://localhost:9200, single-node with security
    disabled, cluster `yumbi-sandbox`. Yumbi detects it and skips spawning a
    TempDb.Elasticsearch container. Startup log: `/tmp/elasticsearch.log`;
    server log: `/var/log/elasticsearch/yumbi-sandbox.log`.

    If any of these are not answering, run
    `/home/agent/.local/bin/start-services` — it is re-entrant and safe to
    repeat.

    mysqld and redis-server are installed for TempDb to spawn; do not start
    them yourself.

    ## Diffity

    `diffity` is installed and its skills are available, but no session is
    running at startup. Comments are anchored to a specific diff, so a session
    started before a commit goes stale as soon as one is made — start a fresh
    session when review is wanted.

    - `/diffity-diff <ref>` starts a session and opens the diff viewer.
    - `/diffity-review <ref>` leaves your own inline comments, tagged
      `[must-fix]`, `[suggestion]`, `[nit]` or `[question]`.
    - `/diffity-resolve` reads all open comments — the user's or your own —
      and applies the requested changes.
    - `diffity agent list --status open --json` reads the current session's
      comments directly.

    There is no browser in this sandbox: always pass `--no-open`, and use the
    default port 5391, which is the only port published to the host.

    After starting a session, tell the user the URL to open. It is in the
    environment variable `VIBE_DIFFITY_URL` — the host port may differ from
    5391 when several sandboxes are running, so report that variable's value
    rather than assuming.

    If the user says they left comments but `diffity agent list` shows none,
    the session's ref no longer matches the diff they commented on (usually
    because of an intervening commit). Say so rather than asking them to
    re-run `/diffity-review`, which only adds your own comments.
SPEC
}

write_kit() {
  mkdir -p -- "$KIT"
  kit_spec > "${KIT}/spec.yaml"
}

# Write the bundled spec only when the caller hasn't provided one.
ensure_kit() {
  local spec="${KIT}/spec.yaml"
  [ ! -f "$spec" ] || return 0
  note "kit spec missing — writing default to ${spec}"
  write_kit
}

# Ask a yes/no question on the terminal. -f answers yes. Reads /dev/tty so a
# redirected stdin can't silently approve a destructive action.
confirm() {
  local prompt="$1" reply=""
  [ "$force" != "1" ] || return 0
  if [ ! -t 0 ] && [ ! -e /dev/tty ]; then
    die "no terminal to confirm on: ${prompt} — re-run with -f"
  fi
  printf "vibe: %s [y/N] " "$prompt" >&2
  read -r reply < /dev/tty || reply=""
  case "$reply" in
    [yY] | [yY][eE][sS]) return 0 ;;
    *) return 1 ;;
  esac
}

# --init / --re-init: (re)write the bundled spec, confirming before clobbering.
init_kit() {
  local spec="${KIT}/spec.yaml"
  if [ -f "$spec" ]; then
    confirm "${spec} already exists. Replace it with the bundled spec?" || {
      note "kit spec left unchanged"
      return 0
    }
  fi
  write_kit
  note "wrote ${spec}"
  [ "$do_reinit" = "1" ] \
    || note "existing sandboxes keep their old kit — 'sbx rm <name>' to pick this up"
}

# Derive a valid sandbox name from a path.
# Rules: lowercase alphanumerics and dashes, must start alphanumeric, min 2 chars.
derive_name() {
  local path="$1" raw
  raw="$(basename -- "$path")"
  raw="$(printf "%s" "$raw" \
    | tr "[:upper:]" "[:lower:]" \
    | sed -E "s/[^a-z0-9]+/-/g; s/^[^a-z0-9]+//; s/-+$//")"
  [ -n "$raw" ] || raw="sandbox"
  [ "${#raw}" -ge 2 ] || raw="${raw}0"
  printf "%s" "$raw"
}

sandbox_exists() {
  sbx ls --quiet 2>/dev/null | grep -qx -- "$1"
}

# Does ssh have a usable config for this host? `ssh -G` prints the effective
# configuration after following Include directives, so we never parse
# ~/.ssh/config ourselves — sbx setup ssh writes its block into an included
# file, not the main one. A matching *.sbx block sets a ProxyCommand; without
# one, ssh would try to resolve "<name>.sbx" as a real hostname and fail at DNS.
# If `ssh -G` is unavailable we return success and let ssh speak for itself.
ssh_host_configured() {
  local host="$1" out
  out="$(ssh -G "$host" 2>/dev/null)" || return 0
  printf "%s\n" "$out" | awk '
    tolower($1) == "proxycommand" && tolower($2) != "none" { found = 1 }
    END { exit found ? 0 : 1 }
  '
}

# Walk upward from BASE_PORT until we find one nothing is listening on.
find_free_port() {
  local port="$1"
  while ss -ltnH "sport = :${port}" 2>/dev/null | grep -q .; do
    port=$((port + 1))
    [ "$port" -lt 5450 ] || die "no free port found in 5391-5449"
  done
  printf "%s" "$port"
}

name_override=""
force="0"
do_init="0"
do_stop="0"
do_reinit="0"
do_ssh="0"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --init) do_init="1"; shift ;;
    --re-init | --reinit) do_reinit="1"; shift ;;
    --stop) do_stop="1"; shift ;;
    --ssh) do_ssh="1"; shift ;;
    -f | --force) force="1"; shift ;;
    -h | --help) usage; exit 0 ;;
    -n | --name)
      [ "$#" -ge 2 ] || die "option $1 requires an argument"
      name_override="$2"; shift 2 ;;
    -n*) name_override="${1#-n}"; shift ;;
    --) shift; break ;;
    -*) die "unknown option: $1" ;;
    *) break ;;
  esac
done

case "${do_init}${do_stop}${do_reinit}${do_ssh}" in
  0000 | 1000 | 0100 | 0010 | 0001) ;;
  *) die "--init, --stop, --re-init and --ssh are mutually exclusive" ;;
esac

if [ "$do_init" = "1" ]; then
  [ "$#" -eq 0 ] || die "--init takes no path argument"
  init_kit
  exit 0
fi

[ "$#" -le 1 ] || die "expected at most one path argument"

target="${1:-$PWD}"
[ -d "$target" ] || die "not a directory: ${target}"
target="$(cd -- "$target" && pwd -P)"

name="${name_override:-$(derive_name "$target")}"

if [ "$do_stop" = "1" ]; then
  sandbox_exists "$name" \
    || die "no sandbox named '${name}' for ${target} — nothing to stop"
  note "stopping sandbox '${name}'"
  exec sbx stop "$name"
fi

if [ "$do_ssh" = "1" ]; then
  sandbox_exists "$name" \
    || die "no sandbox named '${name}' for ${target} — run vibe first"
  command -v ssh >/dev/null 2>&1 || die "ssh not found on PATH"
  ssh_host_configured "${name}.sbx" || die "\
no ssh configuration for ${name}.sbx
(checked ~/.ssh/config and everything it Includes, via 'ssh -G')
Run 'sbx setup ssh' once on this machine."
  note "connecting to ${name}.sbx"
  exec ssh "${name}.sbx"
fi

# The published host port is chosen at creation and must stay stable so the
# diffity tab can be bookmarked. Record it rather than parsing sbx output.
readonly PORT_ROOT="${SBX_CC_PORT_ROOT:-${HOME}/.vibe/ports}"

remember_port() {
  mkdir -p "$PORT_ROOT"
  printf "%s\n" "$2" > "${PORT_ROOT}/$1"
}

recall_port() {
  local f="${PORT_ROOT}/$1"
  if [ -r "$f" ]; then
    cat "$f"
  else
    printf "%s" "$BASE_PORT"
  fi
}

port_in_use() {
  ss -tlnH "sport = :$1" 2>/dev/null | grep -q .
}

report_port_holder() {
  local port="$1"
  note "  processes holding ${port}:"
  ss -tlnp "sport = :${port}" 2>/dev/null | sed 's/^/    /' >&2 || true
  if command -v fuser >/dev/null 2>&1; then
    fuser -v "${port}/tcp" 2>&1 | sed 's/^/    /' >&2 || true
  fi
}

# Wait until the sandbox will accept an exec. sbx create provisions but does
# not start; sbx run boots it. Bounded so a broken sandbox fails loudly.
wait_reachable() {
  local target_name="$1" limit="${2:-120}" i=0
  while [ "$i" -lt "$limit" ]; do
    if sbx exec "$target_name" -- /bin/true >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  return 1
}

# Memory preservation is claude-specific: the path is /home/agent/.claude.
memories_supported() {
  [ "$AGENT" = "claude" ]
}

memory_store_for() {
  printf "%s/%s" "$MEMORY_ROOT" "$1"
}

# Copy the agent's memories out to the mounted host directory before the
# sandbox is destroyed. Requires the sandbox to have been created by a version
# of this script that mounts MEMORY_ROOT.
backup_memories() {
  local target_name="$1" store
  store="$(memory_store_for "$target_name")"
  mkdir -p "$store"
  if ! sbx exec "$target_name" -- test -d "$AGENT_MEMORY_PATH" >/dev/null 2>&1; then
    note "  no memories found in '${target_name}' — nothing to preserve"
    return 1
  fi
  if sbx exec "$target_name" -- sh -c \
      "mkdir -p '$store' && cp -a '$AGENT_MEMORY_PATH'/. '$store'/" >/dev/null 2>&1; then
    note "  memories saved to ${store}"
    return 0
  fi
  note "  WARNING: could not save memories (sandbox may predate memory support)"
  return 1
}

# Copy them back after re-creation, before the agent starts at sbx run.
restore_memories() {
  local target_name="$1" store
  store="$(memory_store_for "$target_name")"
  if [ ! -d "$store" ] || [ -z "$(ls -A "$store" 2>/dev/null)" ]; then
    return 0
  fi
  note "waiting for '${target_name}' to accept commands before restoring memories"
  if ! wait_reachable "$target_name" 120; then
    note "WARNING: sandbox not reachable — memories left in ${store}"
    return 1
  fi
  if sbx exec "$target_name" -- sh -c \
      "mkdir -p '$AGENT_MEMORY_PATH' && cp -a '$store'/. '$AGENT_MEMORY_PATH'/" >/dev/null 2>&1; then
    note "memories restored from ${store}"
    return 0
  fi
  note "WARNING: restore failed — memories left in ${store}"
  return 1
}

restore_after_create="0"

if [ "$do_reinit" = "1" ]; then
  if sandbox_exists "$name"; then
    confirm "Remove sandbox '${name}' (workspace ${target})?" \
      || die "aborted — sandbox left alone"
    if memories_supported; then
      if confirm "Preserve agent memories from '${name}'?"; then
        backup_memories "$name" && restore_after_create="1"
      fi
    fi
    note "removing sandbox '${name}'"
    sbx rm -f "$name"
  else
    note "no existing sandbox '${name}' — nothing to remove"
  fi
  init_kit
  note "rebuilding '${name}' from ${KIT}/spec.yaml"
fi

if sandbox_exists "$name"; then
  note "attaching to existing sandbox '${name}'"
  port="$(recall_port "$name")"
  # A running sandbox already holds its published port; only a stopped one
  # will try to bind it on start, which is when sbx prompts interactively.
  if ! sbx exec "$name" -- /bin/true >/dev/null 2>&1; then
    if port_in_use "$port"; then
      note "host port ${port} is in use, and sandbox '${name}' is stopped."
      note "sbx would prompt for an alternative port and lose your stable URL."
      report_port_holder "$port"
      die "free port ${port} (e.g. 'fuser -k ${port}/tcp'), then re-run"
    fi
  fi
  note "  diffity:   http://localhost:${port}"
else
  ensure_kit
  port="$(find_free_port "$BASE_PORT")"
  note "creating sandbox '${name}'"
  note "  workspace: ${target}"
  note "  memory:    ${MEMORY}"

  # Extra workspace mounts appear inside the sandbox at their exact host path,
  # which is why link-nuget symlinks /home/agent/.nuget to it at startup.
  extra_mounts=()
  create_env=()
  if [ -n "$NUGET_DIR" ] && [ -d "$NUGET_DIR" ]; then
    extra_mounts+=("$NUGET_DIR")
    create_env+=(-e "SBX_HOST_NUGET=${NUGET_DIR}")
    note "  nuget:     ${NUGET_DIR} (shared with host)"
  elif [ -n "$NUGET_DIR" ]; then
    note "  nuget:     ${NUGET_DIR} not found on host — not mounting"
  fi

  if memories_supported; then
    memory_store="$(memory_store_for "$name")"
    mkdir -p "$memory_store"
    extra_mounts+=("$memory_store")
    create_env+=(-e "SBX_CC_MEMORY_STORE=${memory_store}")
    note "  memories:  ${memory_store}"
  fi

  create_env+=(-e "VIBE_DIFFITY_URL=http://localhost:${port}")

  sbx create \
    --name "$name" \
    --kit "$KIT" \
    --memory "$MEMORY" \
    --publish "${port}:${BASE_PORT}" \
    ${create_env[@]+"${create_env[@]}"} \
    "$AGENT" "$target" ${extra_mounts[@]+"${extra_mounts[@]}"}

  if [ "$restore_after_create" = "1" ]; then
    restore_memories "$name" || true
  fi
  remember_port "$name" "$port"
fi

# setup.startup doesn't fire on a sandbox's first boot (the launcher isn't on
# disk yet when start hooks dispatch), so nudge it here. The launcher is
# idempotent — it exits early if the port is already bound — so calling this on
# every attach is harmless.
#
# Output goes to a log rather than the terminal: this runs detached and would
# otherwise print into whatever prompt the user has moved on to.
readonly NUDGE_LOG="${TMPDIR:-/tmp}/vibe-nudge.log"

nudge-services() {
  local target_name="$1" i rc out
  # sbx create provisions but does not start; sbx run boots it. This races
  # that boot, which after a full install pass can take well over a minute.
  for i in $(seq 1 180); do
    if out="$(sbx exec "$target_name" -- /bin/true 2>&1)"; then
      printf "[%s] %s: sandbox reachable after %ss\n" \
        "$(date -Is)" "$target_name" "$i" >>"$NUDGE_LOG"
      break
    fi
    if [ "$i" -eq 1 ] || [ "$((i % 30))" -eq 0 ]; then
      printf "[%s] %s: not reachable (attempt %s): %s\n" \
        "$(date -Is)" "$target_name" "$i" "$out" >>"$NUDGE_LOG"
    fi
    if [ "$i" -eq 180 ]; then
      printf "[%s] %s: gave up after 180s\n" "$(date -Is)" "$target_name" >>"$NUDGE_LOG"
      return 1
    fi
    sleep 1
  done

  # start-services runs link-nuget, start-rabbit, then execs diffity, so it
  # never returns: dispatch it detached. It is re-entrant, so a duplicate
  # invocation from a login shell is harmless.
  printf "[%s] %s: dispatching start-services\n" "$(date -Is)" "$target_name" >>"$NUDGE_LOG"
  if sbx exec -d "$target_name" -- "$SERVICE_STARTER" >>"$NUDGE_LOG" 2>&1; then
    printf "[%s] %s: start-services dispatched\n" "$(date -Is)" "$target_name" >>"$NUDGE_LOG"
  else
    rc=$?
    printf "[%s] %s: start-services FAILED rc=%s\n" \
      "$(date -Is)" "$target_name" "$rc" >>"$NUDGE_LOG"
  fi
}

# All output is redirected: this outlives the foreground session, and would
# otherwise print into whatever prompt the user has moved on to.
nudge-services "$name" </dev/null >/dev/null 2>&1 &

exec sbx run --name "$name"
