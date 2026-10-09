package cliargs

import "testing"

func TestParseBasics(t *testing.T) {
	a, err := Parse([]string{"-n", "custom", "--profile", "yumbi", "/path/to/code"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "custom" || a.Profile != "yumbi" || a.Path != "/path/to/code" {
		t.Errorf("unexpected args: %+v", a)
	}
}

func TestParseLongForms(t *testing.T) {
	a, err := Parse([]string{"--name=foo", "--profile=bar"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "foo" || a.Profile != "bar" {
		t.Errorf("unexpected args: %+v", a)
	}
}

func TestParseStopAll(t *testing.T) {
	for _, flag := range []string{"-S", "--stop-all"} {
		a, err := Parse([]string{flag, "-f"})
		if err != nil || !a.StopAll || !a.Force {
			t.Fatalf("%s: expected StopAll+Force, got %+v err=%v", flag, a, err)
		}
	}
	if a, _ := Parse([]string{"--stop-all", "--stop"}); a.ExclusiveActions() != 2 {
		t.Errorf("--stop-all should count as an exclusive action: %+v", a)
	}
}

func TestParseActionFlags(t *testing.T) {
	a, err := Parse([]string{"-s"})
	if err != nil || !a.Stop {
		t.Fatalf("expected Stop=true, got %+v err=%v", a, err)
	}
	a, err = Parse([]string{"--re-init", "-f"})
	if err != nil || !a.ReInit || !a.Force {
		t.Fatalf("expected ReInit+Force, got %+v err=%v", a, err)
	}
	a, err = Parse([]string{"-R"})
	if err != nil || !a.ReCreate {
		t.Fatalf("expected ReCreate, got %+v err=%v", a, err)
	}
	a, err = Parse([]string{"--re-create"})
	if err != nil || !a.ReCreate {
		t.Fatalf("expected ReCreate, got %+v err=%v", a, err)
	}
	a, err = Parse([]string{"-i"})
	if err != nil || !a.Install {
		t.Fatalf("expected Install, got %+v err=%v", a, err)
	}
	a, err = Parse([]string{"--install"})
	if err != nil || !a.Install {
		t.Fatalf("expected Install, got %+v err=%v", a, err)
	}
}

func TestExclusiveActions(t *testing.T) {
	a, _ := Parse([]string{"--stop", "--ssh"})
	if a.ExclusiveActions() != 2 {
		t.Errorf("expected 2 exclusive actions set, got %d", a.ExclusiveActions())
	}
	a, _ = Parse([]string{"--re-create"})
	if a.ExclusiveActions() != 1 {
		t.Errorf("expected re-create to count as an exclusive action, got %d", a.ExclusiveActions())
	}
	a, _ = Parse([]string{"--install"})
	if a.ExclusiveActions() != 1 {
		t.Errorf("expected install to count as an exclusive action, got %d", a.ExclusiveActions())
	}
}

func TestUnknownOption(t *testing.T) {
	if _, err := Parse([]string{"--nope"}); err == nil {
		t.Error("expected error for unknown option")
	}
}

func TestTooManyPaths(t *testing.T) {
	if _, err := Parse([]string{"/a", "/b"}); err == nil {
		t.Error("expected error for two positional paths")
	}
}

func TestParseReCompose(t *testing.T) {
	for _, arg := range []string{"-C", "--re-compose", "--recompose"} {
		got, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse(%s): %v", arg, err)
		}
		if !got.ReCompose {
			t.Errorf("Parse(%s) did not set ReCompose", arg)
		}
		if got.ExclusiveActions() != 1 {
			t.Errorf("Parse(%s): ExclusiveActions = %d, want 1", arg, got.ExclusiveActions())
		}
	}
	both, err := Parse([]string{"-C", "-R"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("-C with -R should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseCleanup(t *testing.T) {
	for _, arg := range []string{"-x", "--cleanup"} {
		got, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse(%s): %v", arg, err)
		}
		if !got.Cleanup {
			t.Errorf("Parse(%s) did not set Cleanup", arg)
		}
		if got.ExclusiveActions() != 1 {
			t.Errorf("Parse(%s): ExclusiveActions = %d, want 1", arg, got.ExclusiveActions())
		}
	}
	// -f is cleanup's own modifier (skip the confirmation), not a second
	// action, so it must not trip the mutual-exclusion check.
	forced, err := Parse([]string{"--cleanup", "-f"})
	if err != nil || !forced.Cleanup || !forced.Force {
		t.Fatalf("expected Cleanup+Force, got %+v err=%v", forced, err)
	}
	if forced.ExclusiveActions() != 1 {
		t.Errorf("--cleanup -f: ExclusiveActions = %d, want 1", forced.ExclusiveActions())
	}
	both, err := Parse([]string{"-x", "-l"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("-x with -l should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseDelete(t *testing.T) {
	for _, argv := range [][]string{{"-d"}, {"--delete"}, {"--delete", "-f", "some/path"}} {
		got, err := Parse(argv)
		if err != nil {
			t.Fatalf("Parse(%v): %v", argv, err)
		}
		if !got.Delete {
			t.Errorf("Parse(%v) did not set Delete", argv)
		}
		if got.ExclusiveActions() != 1 {
			t.Errorf("Parse(%v): ExclusiveActions = %d, want 1", argv, got.ExclusiveActions())
		}
	}
	both, err := Parse([]string{"-d", "-r"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("-d with -r should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseInfo(t *testing.T) {
	for _, argv := range [][]string{{"-a"}, {"--info"}, {"--info", "some/path"}} {
		got, err := Parse(argv)
		if err != nil {
			t.Fatalf("Parse(%v): %v", argv, err)
		}
		if !got.Info || got.ExclusiveActions() != 1 {
			t.Errorf("Parse(%v) = %+v; want Info, as the one action", argv, got)
		}
	}
	both, err := Parse([]string{"-a", "-d"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("-a with -d should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseVersion(t *testing.T) {
	for _, arg := range []string{"-v", "--version"} {
		got, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse(%s): %v", arg, err)
		}
		if !got.Version {
			t.Errorf("Parse(%s) did not set Version", arg)
		}
		if got.ExclusiveActions() != 0 {
			t.Errorf("Parse(%s): ExclusiveActions = %d, want 0 — like --help, it just answers", arg, got.ExclusiveActions())
		}
	}
}

func TestParseUpdateStrategy(t *testing.T) {
	for _, argv := range [][]string{
		{"-i", "-u", "merge,keep"},
		{"-i", "--update-strategy", "merge,keep"},
		{"-i", "--update-strategy=merge,keep"},
		{"-i", "-umerge,keep"},
	} {
		got, err := Parse(argv)
		if err != nil {
			t.Fatalf("Parse(%v): %v", argv, err)
		}
		if !got.Install || got.UpdateStrategy != "merge,keep" {
			t.Errorf("Parse(%v) = %+v, want Install with strategy merge,keep", argv, got)
		}
	}
	if _, err := Parse([]string{"-i", "--update-strategy"}); err == nil {
		t.Error("--update-strategy with no value was accepted")
	}
}

func TestParseUpgrade(t *testing.T) {
	for _, arg := range []string{"-U", "--upgrade"} {
		got, err := Parse([]string{arg, "-u", "merge,keep"})
		if err != nil || !got.Upgrade || got.UpdateStrategy != "merge,keep" {
			t.Errorf("Parse(%s -u merge,keep) = %+v, %v", arg, got, err)
		}
		if got.ExclusiveActions() != 1 {
			t.Errorf("--upgrade counts as %d actions, want 1", got.ExclusiveActions())
		}
	}
	if both, _ := Parse([]string{"-U", "-i"}); both.ExclusiveActions() != 2 {
		t.Error("--upgrade with --install should be rejected as two actions")
	}
}

func TestParseInstallSbx(t *testing.T) {
	for _, arg := range []string{"-I", "--install-sbx"} {
		got, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse(%s): %v", arg, err)
		}
		if !got.InstallSbx {
			t.Errorf("Parse(%s) did not set InstallSbx", arg)
		}
		if got.ExclusiveActions() != 1 {
			t.Errorf("Parse(%s): ExclusiveActions = %d, want 1", arg, got.ExclusiveActions())
		}
	}
	both, err := Parse([]string{"-I", "-U"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("-I with -U should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseNoCompanion(t *testing.T) {
	for _, flag := range []string{"-N", "--no-companion"} {
		a, err := Parse([]string{flag, "/path/to/code"})
		if err != nil || !a.NoCompanion || a.Path != "/path/to/code" {
			t.Fatalf("%s: expected NoCompanion and the path, got %+v err=%v", flag, a, err)
		}
		if a.ExclusiveActions() != 0 {
			t.Errorf("%s is not an action", flag)
		}
	}
}

func TestParseSetup(t *testing.T) {
	got, err := Parse([]string{"--setup"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Setup || got.ExclusiveActions() != 1 {
		t.Errorf("Parse(--setup) = %+v; want Setup, as the one action", got)
	}
	both, err := Parse([]string{"--setup", "-a"})
	if err != nil {
		t.Fatal(err)
	}
	if both.ExclusiveActions() != 2 {
		t.Errorf("--setup with -a should count as two actions, got %d", both.ExclusiveActions())
	}
}

func TestParseNoStart(t *testing.T) {
	got, err := Parse([]string{"--no-start", "-C", "-f"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.NoStart || !got.ReCompose || !got.Force {
		t.Errorf("got %+v, want NoStart, ReCompose and Force set", got)
	}
	if got.ExclusiveActions() != 1 {
		t.Errorf("--no-start should not count as an action, got %d", got.ExclusiveActions())
	}
}
