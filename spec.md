VIBE
---

What is it?
---
A program to easily create, start and attach to docker sbx sessions for contained agentic coding

The aim is to provide configuration packs which set up the agent's environment sufficiently to
be able to read, write and compile code as well as run any related tests.

User flow
---

The app either runs in the current folder, or uses a user-provided override folder to run in.
The folder is projected into a docker sbx session and some kits are installed. In addition, there
are scripts to run once, and scripts for startup, since the container has no init system.

The app is built with go to be cross-platform and performant, as well as to allow for an easier
download-and-use route for the user, since we can build platform-native binaries for windows,linux
and osx.

The app would be bundled in a zip file, with some starter profiles. Profiles are stored as a folder
containing items to be used during creation, for example, let's consider the profile for yumbi: if
we unpack the app and data, we would see a directory listing like:

vibe-unpacked/
    vibe
    readme.md
    settings.yaml
    config.yaml
    /defaults
        /install-scripts
        /agent-files
            /.local
                /.bin
                    install-diffity
                    add-diffity-skills
    /profiles
        /yumbi
            config.yaml
            settings.yaml
            /install-scripts
                01-refresh-apt-indices
                02-install-dotnet
                03-install-services
                04-install-asp-net8.0-runtime
                05-install-diffity
                06-add-diffity-skills
                07-enable-elasticsearch-repo
                08-install-elasticsearch
                09-configure-elasticsearch
            /agent-files
                /.claude
                    settings.json
                /.local
                    /bin
                        start-rabbit
                        link-nuget
                        start-elasticsearch
                        on-start
            agent-instructions.md

The included vibe.sh does this in bash, and I'd like to make this easily extendable via profiles
for other repos and so other people can use this. The start/stop messages should always be included.
The scripts that are run should be sourced from install-scripts. When a script has a commented `# Description:`
near the top of the file, that description should be displayed, otherwise the path to the script
should be displayed whilst running the script at creation time.

The user should be able to simply start up with `vibe` in a folder. The profile name should be picked up from
the leaf node name of the folder (eg, for `/home/user/code/company/foo-browser`, the profile would be
`foo-browser`). The profile may be specifically selected with the cli parameter `--profile`, eg 
`vibe --profile yumbi`. All other cli parameters already supported by the example script must also
be supported:
- `vibe /path/to/code` should open vibe in `/path/to/code` instead of the current working directory
- `vibe -n <custom name>` should override the name of the generated sandbox, and, if there is no `--profile`
   provided on the cli, `<custom name>` should be used for the profile name too. It should also support, eg
   `vibe --name foo --profile bar` to load the `bar` profile, but create the container with the name `foo`,
   for example.
- for all commands, when no sandbox name is provided, the name should be derived from the folder on which
  `vibe` is operating. When a sandbox name is provided, it should override that derived name.
- `vibe --stop` should stop the vibe session for the implied sandbox name for the current folder. It should
  also support `vibe --stop /path/to/code` (stopping the sandbox by the default name for that folder) and
  `vibe --stop --name foo` to stop the `foo` sandbox. 
- `vibe --ssh` should connect to the docker sbx container for the derived name of the current folder;
  similarly, `vibe --ssh /path/to/code` should connect to the sbx container for `/path/to/code`, and
  `vibe --ssh --name foo` should connect to the `foo` sandbox.
- `vibe --re-init` should re-initialise the sandbox from scratch, using the same profile, prompting
  the user like the current script does
- `vibe --re-init -f` should just go ahead and re-initialise, assuming yes to all prompts.

When the selected profile doesn't exist — the usual case the first time `vibe` runs in a folder,
since the profile name is derived from the folder name — the app must not fail with "unknown
profile". It should say no profile was found and offer to create one: either a copy of an existing
profile (copying the whole profile directory and renaming it) or a new blank profile (the minimum
a profile needs: a `config.yaml` naming it), written to `~/.vibe/profiles`. `-f`/`--force` answers that prompt with the blank
profile, and where there is no terminal to prompt on, the app explains what to run instead rather
than guessing.

In addition, I'd like `vibe --list` to list all known sandbox containers and their current status, eg:
```
vibe --list
yumbi       running
phoenix     not running
```

All cli options should have both a long and short form, eg `-n` and `--name`

Where the data lives:

The app's own folder (the unpacked bundle) ships the defaults, but the master copy of the
configuration is `~/.vibe` (overridable with `$VIBE_HOME`), which overlays the bundle:

- Any file present in `~/.vibe` overrides the same relative-pathed file in the app's folder. There
  is no merging of the two — the `~/.vibe` copy wins outright, so `~/.vibe/settings.yaml` replaces
  the bundle's `settings.yaml`.
- The two directories of many files — `defaults` and each `profiles/<name>` — override a whole
  directory at a time instead: if `~/.vibe/defaults` or `~/.vibe/profiles/valleyrunner` exists, it
  completely replaces the bundle's, with no per-file fall-back.
- When the app starts and finds no `~/.vibe` configuration, it creates the folder, copies
  `config.yaml`, `settings.yaml` and the whole `defaults` folder into it, and prompts per profile
  to copy the bundle's profiles into `~/.vibe/profiles`.
- Profiles the app creates are written to `~/.vibe/profiles`, never into the bundle.

File formats:
1. settings.yaml in the root of the vibe app and in the base of each profile is used to configure
    the settings that are used as part of the sandbox setup. The final config to use would be to take
    the settings.yaml in the root folder, and, if there is a settings.yaml in the profile folder,
    apply that as overrides to the base, and use that for sandbox creation.
For example, some settings which may appear in /settings.yaml:
```
- memory: 12g
- publish:
  - service 1
    - 1234
  - service 2
    - 4567
    - 4568
```
which configures the sandbox memory max to 12 gig and opens the two ports 1234 and 4567
2. config.yaml should conform to the docker sbx yaml format. At the time of creating a new
    sandbox, the app should create a temporary config, stored in /tmp, to start up the sandbox.
    The app should generate this config from the profile config, overlaid on top of the base config
    and with the `install` and `files` sections generated from the profile folder. If there is
    an `on-start` script in the profile folder, that should be added to the `startup:` section
    in the generated config, run as user 0, backgrounded, with the description "running startup script"
3. The `install` section is generated from profile files: each file under install-script is inlined
   into run in sequence, ordered by the leading index number (eg 1 for refresh-apt-indices). 
4. the app should generate a script like the existing one and store it in `~/.vibe/start-<sandbox-name>`,
    eg `~/.vibe/start-yumbi`. This script should be used to launch the sandbox every time after creation,
    when it exists. A `--re-init` would re-generate that file from the selected profile, to allow
    the user to make modifications and recreate the sandbox. Note that a re-init allows for restoring
    agent memories, so these have to be gleaned from the sandbox before attempting re-initialisation,
    if the sandbox can be found

