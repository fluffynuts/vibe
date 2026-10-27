package cursorstream

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// translated is every line of a sample turn, as Translate gives it.
func translated(t *testing.T, sample string) []map[string]any {
	t.Helper()
	f, err := os.Open("../../docs/samples/cursor/" + sample)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	in := bufio.NewScanner(f)
	in.Buffer(nil, 4<<20)
	for in.Scan() {
		for _, line := range Translate(in.Bytes()) {
			var o map[string]any
			if err := json.Unmarshal(line, &o); err != nil {
				t.Fatalf("not JSON: %s", line)
			}
			out = append(out, o)
		}
	}
	return out
}

func blocks(o map[string]any) []map[string]any {
	var out []map[string]any
	for _, b := range o["message"].(map[string]any)["content"].([]any) {
		out = append(out, b.(map[string]any))
	}
	return out
}

func TestATurnWithToolsIsTheSequenceThePageDraws(t *testing.T) {
	var seen []string
	for _, o := range translated(t, "turn-with-tools.jsonl") {
		kind := o["type"].(string)
		if kind == "assistant" || kind == "user" {
			kind += ":" + blocks(o)[0]["type"].(string)
		}
		seen = append(seen, kind)
	}
	got := strings.Join(seen, " ")
	want := "system assistant:text assistant:tool_use user:tool_result assistant:tool_use user:tool_result assistant:tool_use user:tool_result assistant:text result"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestToolsAreNamedAndShownByWhatTheyWereAskedAndTold(t *testing.T) {
	var uses, results []map[string]any
	for _, o := range translated(t, "turn-with-tools.jsonl") {
		if o["type"] == "assistant" && blocks(o)[0]["type"] == "tool_use" {
			uses = append(uses, blocks(o)[0])
		}
		if o["type"] == "user" {
			results = append(results, blocks(o)[0])
		}
	}
	names := []any{uses[0]["name"], uses[1]["name"], uses[2]["name"]}
	if names[0] != "Read" || names[1] != "Edit" || names[2] != "Shell" {
		t.Fatalf("names: %v", names)
	}
	if uses[2]["input"].(map[string]any)["command"] != "ls" {
		t.Fatalf("shell input: %v", uses[2]["input"])
	}
	if results[0]["content"] != "# hello\nA tiny project.\n" || results[2]["content"] != "README.md\nnotes.txt\n" {
		t.Fatalf("results: %v / %v", results[0]["content"], results[2]["content"])
	}
	for i, r := range results {
		if r["is_error"] != false {
			t.Errorf("result %d marked as an error", i)
		}
		if r["tool_use_id"] != uses[i]["id"] {
			t.Errorf("result %d answers %v, not the tool used %v", i, r["tool_use_id"], uses[i]["id"])
		}
	}
}

func TestFailedToolsAreErrorsWithWhatWentWrong(t *testing.T) {
	var results []map[string]any
	for _, o := range translated(t, "turn-tool-failures.jsonl") {
		if o["type"] == "user" {
			results = append(results, blocks(o)[0])
		}
	}
	if len(results) != 2 {
		t.Fatalf("%d results", len(results))
	}
	if results[0]["is_error"] != true || !strings.Contains(results[0]["content"].(string), "No such file or directory") {
		t.Errorf("shell failure: %v", results[0])
	}
	if results[1]["is_error"] != true || results[1]["content"] != "File not found" {
		t.Errorf("read error: %v", results[1])
	}
}

func TestTheEchoOfThePromptAndThinkingAreLeftOut(t *testing.T) {
	for _, o := range translated(t, "turn-text-only-partial.jsonl") {
		if o["type"] == "user" || o["type"] == "thinking" {
			t.Fatalf("let through: %v", o)
		}
	}
}

func TestInitAndResultGoThroughAsTheyAre(t *testing.T) {
	lines := translated(t, "turn-resumed.jsonl")
	first, last := lines[0], lines[len(lines)-1]
	if first["type"] != "system" || first["session_id"] == "" || first["model"] != "Auto" {
		t.Errorf("init: %v", first)
	}
	if last["type"] != "result" || last["result"] != "`notes.txt`" || last["is_error"] != false {
		t.Errorf("result: %v", last)
	}
}

func TestWhatItDoesNotKnowGoesThroughAsItIs(t *testing.T) {
	for _, in := range []string{`{"type":"something_new","x":1}`, `not json`, `{"type":"tool_call","subtype":"started","tool_call":{}}`} {
		out := Translate([]byte(in))
		if len(out) != 1 || string(out[0]) != in {
			t.Errorf("%q became %q", in, out)
		}
	}
}

func TestAToolResultOfAShapeNotSeenYetIsAnErrorWithItsFields(t *testing.T) {
	line := `{"type":"tool_call","subtype":"completed","call_id":"c","session_id":"s","tool_call":{"grepToolCall":{"args":{},"result":{"rejected":{"reason":"nope"}}}}}`
	out := Translate([]byte(line))
	var o map[string]any
	json.Unmarshal(out[0], &o)
	b := blocks(o)[0]
	if b["is_error"] != true || !strings.Contains(b["content"].(string), "nope") {
		t.Fatalf("%v", b)
	}
}
