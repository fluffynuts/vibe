package main

// runningWork is what clearToChange needs of the sandboxes and vibes running,
// and of the user: apart, so that tests can stand in for them.
type runningWork struct {
	// find is what runs now (see findRunning); stop stops it (see stopRunning).
	find func() (stopAllPlan, error)
	stop func(stopAllPlan) error
	// ask asks a yes/no question, no being the answer to just pressing enter.
	ask func(question string) bool
}

func realRunningWork(vibeHome string) runningWork {
	return runningWork{
		find: func() (stopAllPlan, error) { return findRunning(vibeHome) },
		stop: func(p stopAllPlan) error { return stopRunning(vibeHome, p) },
		ask:  func(question string) bool { return confirmDefault(false, false, question) },
	}
}

// clearToChange gets what is named (Docker SBX, or vibe) clear to be
// upgraded or downgraded, saying whether it is. Changing sbx restarts its
// daemon, which pulls every running sandbox from under it, and a restart with
// sandboxes running has been seen to hang; going back on vibe changes the
// files running vibes use. So whatever --stop-all would stop is stopped
// first, as it would be (vibes ending, their agents' memories saved): with -f
// (force) unasked, else once the user says so (canAsk: there is a terminal to
// ask on). Said no to, or with nobody to ask, nothing is stopped, and what is
// named is not clear.
func clearToChange(w runningWork, what string, force, canAsk bool) (bool, error) {
	plan, err := w.find()
	if err != nil {
		return false, err
	}
	if len(plan.names) == 0 {
		return true, nil
	}
	note("%s can't be changed while sandboxes run, and these are running: %s", what, plan.describe())
	switch {
	case force:
		note("stopping them first (-f), as 'vibe --stop-all' does")
	case !canAsk:
		note("there is no terminal to ask on whether to stop them, so they are left running; -f stops them first")
		return false, nil
	case !w.ask("Stop them all first, as 'vibe --stop-all' does (their vibes end, saving their agents' memories)?"):
		note("left running: stop them ('vibe --stop-all') and run this again to change %s", what)
		return false, nil
	}
	if err := w.stop(plan); err != nil {
		return false, err
	}
	return true, nil
}
