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
