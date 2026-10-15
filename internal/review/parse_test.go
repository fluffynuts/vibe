package review

import (
	"strings"
	"testing"
)

func TestAModifiedFilesLinesCarryBothLineNumbers(t *testing.T) {
	files, err := Parse(strings.Join([]string{
		"diff --git a/main.go b/main.go",
		"index 1111111..2222222 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -2,3 +2,4 @@ func main() {",
		" a",
		"-b",
		"+B",
		"+c",
		" d",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("%d files", len(files))
	}
	f := files[0]
	if f.Path != "main.go" || f.Status != Modified || len(f.Hunks) != 1 {
		t.Fatalf("file: %+v", f)
	}
	h := f.Hunks[0]
	if h.OldStart != 2 || h.NewStart != 2 || h.Section != "func main() {" {
		t.Errorf("hunk: %+v", h)
	}
	want := []Line{
		{Kind: Context, Old: 2, New: 2, Text: "a"},
		{Kind: Deletion, Old: 3, Text: "b"},
		{Kind: Addition, New: 3, Text: "B"},
		{Kind: Addition, New: 4, Text: "c"},
		{Kind: Context, Old: 4, New: 5, Text: "d"},
	}
	if len(h.Lines) != len(want) {
		t.Fatalf("lines: %+v", h.Lines)
	}
	for i := range want {
		if h.Lines[i] != want[i] {
			t.Errorf("line %d: %+v, want %+v", i, h.Lines[i], want[i])
		}
	}
}

func TestAddedDeletedRenamedAndBinaryFilesAreToldApart(t *testing.T) {
	files, err := Parse(strings.Join([]string{
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"index 0000000..1111111",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1 @@",
		"+hello",
		"diff --git a/gone.txt b/gone.txt",
		"deleted file mode 100644",
		"index 1111111..0000000",
		"--- a/gone.txt",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-bye",
		"diff --git a/old name.txt b/new name.txt",
		"similarity index 100%",
		"rename from old name.txt",
		"rename to new name.txt",
		"diff --git a/logo.png b/logo.png",
		"index 1111111..2222222 100644",
		"Binary files a/logo.png and b/logo.png differ",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Status+" "+f.OldPath+" -> "+f.Path)
	}
	want := []string{
		"added  -> new.txt",
		"deleted gone.txt -> gone.txt",
		"renamed old name.txt -> new name.txt",
		"modified logo.png -> logo.png",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !files[3].Binary || files[0].Binary {
		t.Error("binary isn't told apart")
	}
	if l := files[0].Hunks[0].Lines[0]; l.Kind != Addition || l.New != 1 || l.Text != "hello" {
		t.Errorf("the added file's line: %+v", l)
	}
	if l := files[1].Hunks[0].Lines[0]; l.Kind != Deletion || l.Old != 1 || l.Text != "bye" {
		t.Errorf("the deleted file's line: %+v", l)
	}
}

func TestAQuotedPathIsUnquoted(t *testing.T) {
	files, err := Parse(strings.Join([]string{
		`diff --git "a/tab\there.txt" "b/tab\there.txt"`,
		"index 1111111..2222222 100644",
		`--- "a/tab\there.txt"`,
		`+++ "b/tab\there.txt"`,
		"@@ -1 +1 @@",
		"-a",
		"+b",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Path != "tab\there.txt" {
		t.Errorf("path %q", files[0].Path)
	}
}

func TestALineWithoutANewlineAtTheEndIsntALineOfItsOwn(t *testing.T) {
	files, err := Parse(strings.Join([]string{
		"diff --git a/f b/f",
		"--- a/f",
		"+++ b/f",
		"@@ -1 +1 @@",
		"-a",
		`\ No newline at end of file`,
		"+b",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(files[0].Hunks[0].Lines); n != 2 {
		t.Errorf("%d lines: %+v", n, files[0].Hunks[0].Lines)
	}
}
