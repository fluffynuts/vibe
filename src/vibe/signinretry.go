package main

import (
	"context"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/signin"
)

// retryAfterSignIn says whether a command that failed should be run again: it failed because sbx isn't signed in to Docker,
// it is one that changes nothing, and the user has just been offered the sign-in, on the terminal, and has signed in. It is
// a reaction to the failure rather than a look at sbx before every command (which only the commands that start or make a
// sandbox do, see getsSbxReadyFirst), and it does nothing with no terminal, or with -f, where nobody can be asked.
func retryAfterSignIn(err error, args cliargs.Args, haveTerminal bool, steps sbxSignInSteps) bool {
	if err == nil || !haveTerminal || args.Force || !changesNothing(args) {
		return false
	}
	if signin.Docker(err.Error(), err, false).State != signin.SignedOut {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return offerSbxSignIn(ctx, steps)
}

// changesNothing says args is a command that only reads, which can be run again without doing anything twice.
func changesNothing(args cliargs.Args) bool {
	return args.ExclusiveActions() == 1 && (args.List || args.Info)
}
