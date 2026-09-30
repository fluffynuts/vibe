# Testers wanted

vibe now builds for Linux, macOS and Windows. The Windows build compiles and passes `go vet`, but
**no one has run it on a real Windows machine yet**. Linux and macOS are mostly exercised already,
but the changes touched code they share with Windows (terminal prompts, stopping another vibe),
so a quick check there helps too.

If you can spare half an hour on any of these platforms, please work through the section for your
platform and report what you find (see [Reporting](#reporting)).

## Before you start

You need:

- [Docker Sandboxes (`sbx`)](https://github.com/docker/sbx-releases), installed and working:
  `sbx ls` should run without errors.
- Go (the version in `go.mod` or newer).
- A throwaway folder with some code in it to point vibe at. It creates a sandbox for that folder,
  and at the end you can delete it with `vibe -x`.

Build from the repo root:

| Platform           | Build                  | Run tests             |
| ------------------ | ---------------------- | --------------------- |
| Linux / macOS      | `make` or `./make.sh`  | `./make.sh check`     |
| Windows (PowerShell) | `./make.ps1`         | `./make.ps1 check`    |

`check` runs `go vet` and then `go test ./...`. **Please include its output in your report even
if it passes.** Some tests skip on Windows because they rely on shell scripts or executable bits;
skips are expected, failures are not.

vibe keeps its state in `~/.vibe` (on Windows, `%USERPROFILE%\.vibe`). Two folders there matter
for these tests:

- `running/` holds a `<pid>.lock` file for each vibe that is running, and briefly a `<pid>.stop`
  file when one vibe asks another to stop.
- `memories/<sandbox>/` holds the agent's memories backed up when a session ends.

## Tests for every platform

### 1. First run and profile creation

1. Run `vibe` in your throwaway folder.
2. On a first run it asks whether to copy each bundled profile. Answer `y` and `n` at least once
   each.
3. When it says there is no profile yet, use the arrow-key list to pick "create a new blank
   profile".

**Expect:** each question waits for your answer. The arrow list highlights the current item, moves
with ↑/↓ and redraws in place without leaving copies of itself or stray characters like `[A`.

### 2. Interactive pickers

Use `vibe -x` (cleanup): it shows a checklist and asks you to confirm.

- ↑/↓ move, space toggles, enter goes to the confirmation step.
- Press **Esc on its own**. It should cancel straight away. A hang, or needing a second key, is a
  bug.
- Try `q` and **Ctrl-C** as well. Both should cancel, and your terminal should still work normally
  afterwards: typing echoes, and Enter starts a new line.
- If you meet a reorder list (the guided setup has one), try **Shift+↑/↓** to move an item. Some
  terminals don't send Shift with arrow keys; note which terminal you used if it doesn't work.

### 3. Session, then memory backup

1. Start `vibe` in the throwaway folder and let the session come up.
2. Ask the agent to remember something, e.g. "remember that my favourite colour is teal".
3. Exit the agent normally.

**Expect:** on exit vibe backs up memories, and `~/.vibe/memories/<sandbox>/` contains files. Then
run `vibe -r` (re-init) in the same folder, and in the new session ask the agent what your
favourite colour is. It should remember.

### 3a. Ctrl-C while memories are being saved

Ctrl-C is how you leave a session, so it's easy to keep pressing it after the session has ended,
while vibe is saving the agent's memories. vibe should ask before quitting.

1. Start a session, make sure the agent has a memory saved (as in test 3), and note what's in
   `~/.vibe/memories/<sandbox>/`.
2. Leave the session with Ctrl-C and **keep pressing Ctrl-C** a few times.

**Expect:** once *"Backing up memories to …"* appears, Ctrl-C does not kill vibe. Instead it asks
*"vibe is currently exporting memories from the sandbox - are you sure you want to quit? [y/N]"*.
Pressing Ctrl-C again at that question just asks again.

- Answer **`n`** (or just press Enter). vibe carries on and finishes with *"Backed up memories to
  …"*. If the copy had already finished while the question was up, it says *"Memories were saved
  to …"* instead.
- Run it again and answer **`y`**. vibe prints *"Original local memories restored"* and exits.
  `~/.vibe/memories/<sandbox>/` should hold exactly what it did before this session: nothing from
  the interrupted copy. No `.<sandbox>.before-save-*` folder should be left in
  `~/.vibe/memories/`.

The copy is often quick, so you may need to press Ctrl-C promptly to catch it. With a lot of
memories, or a slow machine, it's easier.

### 4. A second vibe in the same folder

1. Start `vibe` in the throwaway folder, in terminal A.
2. In terminal B, run `vibe` in the same folder.
3. Terminal B should say another vibe is running there and offer to stop it. Choose to stop it.

**Expect:** within a few seconds terminal A's session ends with *"session ended: asked to stop by
another vibe for this folder"*, and A saves its memories before exiting. Terminal B reports
*"Stopped vibe (PID …)"* and carries on. After A has stopped, `~/.vibe/running/` should hold B's
`.lock` file only, with no `.stop` file left over.

Also try it with terminal A **sitting at a prompt** (e.g. on the first-run questions) instead of in
a session. A should print *"asked to stop by another vibe … exiting"* and quit.

If B instead says *"Killed vibe (PID …): it did not stop within 1m0s"*, the stop request didn't get
through. That's a bug; please report it.

### 5. Ending a session abruptly

1. Start a session, then close the terminal window (or kill vibe from another terminal).
2. Run `vibe` in the same folder again.

**Expect:** it starts normally. It should **not** claim another vibe is still running there. A
leftover `.lock` in `~/.vibe/running/` is fine as long as vibe doesn't treat it as a live one.

### 6. `vibe --install`

Run `vibe --install`, then open a new terminal and run `vibe --help` from any directory.

**Expect:** the binary is copied to `~/.local/bin`. If that folder isn't on your PATH, vibe warns
you and shows how to add it, and the example it gives suits your shell (see the platform
sections).

### 7. Port conflict diagnostics

This one is optional. Start something listening on a port a profile publishes, then start that
profile's sandbox. When vibe reports the clash, it should list which process holds the port (see
the platform sections for which tool it uses). If the tool is missing it should skip that part
quietly, never fail.

## Windows

This platform is the priority. Please say which terminal you used (Windows Terminal, the old
`conhost` console, VS Code's terminal, and so on) and which shell (PowerShell 5, PowerShell 7,
cmd).

### Terminal behaviour

- **Colours and redraws:** does any output show raw escape codes like `←[2K` or `\x1b[32m`
  instead of colour? If so, note which terminal it was. vibe turns on escape-code (VT) handling
  itself, but older consoles may not support it.
- **Ctrl-C at the memory-save question** (test 3a): Windows ends a console read on Ctrl-C
  instead of carrying on, and vibe handles that separately. Check that Ctrl-C at the question asks
  again rather than being taken as an answer.
- **Arrow keys, Esc, Shift+arrows** (test 2) are the riskiest part on Windows. The console reports
  keys differently from Unix terminals. Please try a lone Esc several times.
- **Resizing the window while a picker is open** shouldn't change the selection or break the
  list. At worst the next keypress may seem slightly delayed.
- **Redirected input** must never answer a prompt: running `echo y | vibe` should still ask on the
  console, or say there is no terminal. It must not take the `y`.

### Memory backup (test 3)

This is the least certain part. A sandbox runs Linux, so your `C:\Users\…\.vibe\memories\…` folder
shows up inside it under a different path, and `sbx` doesn't document what that path is. vibe
tries several likely forms and checks each one.

- If backup or restore fails with *"could not find C:\… inside the sandbox"*, please run the
  following and include the output:

  ```powershell
  sbx exec <sandbox-name> -- sh -c 'mount; ls /; ls /mnt /c /host_mnt /run/desktop/mnt/host 2>&1'
  ```

  `vibe -l` shows the sandbox name.
- If it works, that's good news too. Please say so.

### Stopping another vibe (test 4)

On Windows the other vibe's `sbx` session is ended by killing the `sbx` process (Windows has no
gentle equivalent of SIGTERM). Please check:

- The sandbox itself keeps running afterwards (`vibe -l`) and can be reattached.
- Terminal A doesn't leave its console in a broken state.

### PATH and install (test 6)

- `vibe --install` should suggest a PowerShell `[Environment]::SetEnvironmentVariable(...)`
  command. Run it, open a **new** terminal, and check that `vibe` is found.
- If `~\.local\bin` is already on PATH but spelled differently (different case, or a trailing
  `\`), vibe should **not** warn.
- Paths starting with `~\` (e.g. `vibe ~\code\project`) should resolve under your user folder.

### Build scripts

- Run `./make.ps1`, `./make.ps1 test` and `./make.ps1 clean`, and a second `./make.ps1` straight
  after a build, which should say *"'vibe.exe' is up to date."*
- If PowerShell refuses to run the script because of its execution policy, note that, then use
  `powershell -ExecutionPolicy Bypass -File make.ps1`.

### Port diagnostics (test 7)

These come from `netstat -ano -p TCP`, filtered down to listeners on the port in question.

## macOS

- **Pickers (test 2):** try Terminal.app and iTerm2 if you have both. Pay attention to a lone Esc,
  and to Shift+arrow reordering, which Terminal.app may not pass through.
- **Stopping another vibe (test 4):** this no longer uses signals; one vibe leaves a request file
  for the other. It's worth confirming that it still works on macOS.
- **Port diagnostics (test 7):** these come from `lsof -nP -iTCP:<port> -sTCP:LISTEN`.
- **Build:** check that `make` and `./make.sh` both work. The system bash is 3.2, which the script
  should support; say if it doesn't.

## Linux

Most development happens on Linux, so a short pass is enough:

- **Stopping another vibe (test 4) and abrupt exit (test 5):** these moved from signals to request
  files, so they're worth a run.
- **Pickers (test 2):** a quick run, ideally inside tmux or screen as well as a plain terminal.
- **Port diagnostics (test 7):** these come from `ss -tlnp` and, if it's installed, `fuser -v`.

## Reporting

Please open an issue (or message the maintainer) with:

- OS and version, terminal and shell.
- `sbx version` output.
- Output of `./make.sh check` or `./make.ps1 check`.
- For each numbered test: pass, fail or skipped. For failures, what you did, what you expected and
  what happened, with the terminal output copied as text where you can (screenshots are fine for
  drawing glitches).
- The contents of `~/.vibe/running/` if test 4 or 5 misbehaved.

Thank you!
