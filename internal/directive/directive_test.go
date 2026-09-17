package directive

import "testing"

func TestParse(t *testing.T) {
	content := []byte(`#!/bin/sh
# vibe: description: Do the thing
# vibe: user: 1000
echo hello
`)
	d := Parse(content)
	if got := d.Description("fallback"); got != "Do the thing" {
		t.Errorf("Description() = %q, want %q", got, "Do the thing")
	}
	if got := d.String("user", "0"); got != "1000" {
		t.Errorf("String(user) = %q, want %q", got, "1000")
	}
}

func TestParseFallback(t *testing.T) {
	d := Parse([]byte("echo hello\n"))
	if got := d.Description("fallback"); got != "fallback" {
		t.Errorf("Description() = %q, want fallback", got)
	}
	if got := d.String("user", "0"); got != "0" {
		t.Errorf("String(user) = %q, want default 0", got)
	}
}

func TestBool(t *testing.T) {
	d := Parse([]byte("# vibe: onlyIfMissing: true\n"))
	if !d.Bool("onlyifmissing", false) {
		t.Error("expected onlyifmissing to parse as true")
	}
	d2 := Parse([]byte("no directives here\n"))
	if d2.Bool("onlyifmissing", false) {
		t.Error("expected default false when directive absent")
	}
}

func TestStripRemovesDirectiveLinesOnly(t *testing.T) {
	content := []byte(`{
  "a": 1
}
`)
	stripped := Strip(content)
	if string(stripped) != string(content) {
		t.Errorf("Strip() changed content with no directives: %q", stripped)
	}

	withDirective := []byte("# vibe: onlyIfMissing: true\n{\n  \"a\": 1\n}\n")
	stripped2 := Strip(withDirective)
	if string(stripped2) != "{\n  \"a\": 1\n}\n" {
		t.Errorf("Strip() = %q", stripped2)
	}
}

func TestParsePlain(t *testing.T) {
	d := ParsePlain([]byte("onlyIfMissing: true\ndescription: hello world\n"))
	if !d.Bool("onlyifmissing", false) {
		t.Error("expected onlyifmissing true from sidecar")
	}
	if got := d.Description("x"); got != "hello world" {
		t.Errorf("Description() = %q", got)
	}
}

func TestMerge(t *testing.T) {
	a := Set{"user": "0", "description": "a"}
	b := Set{"description": "b", "onlyifmissing": "true"}
	merged := a.Merge(b)
	if merged["user"] != "0" {
		t.Error("expected user to survive from a")
	}
	if merged["description"] != "b" {
		t.Error("expected b's description to win")
	}
	if merged["onlyifmissing"] != "true" {
		t.Error("expected onlyifmissing from b")
	}
}
