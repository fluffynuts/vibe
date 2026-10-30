package main

// sayFirstSteps tells someone whose install has just finished, the first on this machine, how to use vibe: nothing in the
// install says what comes next, and "vibe" in the wrong folder is easy to run. A later install (an upgrade) has no need.
func sayFirstSteps(firstInstall bool) {
	if !firstInstall {
		return
	}
	note("")
	note("vibe is installed. To start working on a project:")
	note("  cd into its folder and run: vibe")
	note("  or, from anywhere, give it the folder: vibe /path/to/folder")
	note("The first time in a folder it asks a few questions and builds a sandbox for it, which takes a few minutes.")
	note("Then it prints the address of its companion page, where you chat with the agent.")
}
