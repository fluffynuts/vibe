package opencodestream

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// translated is every line of a real OpenCode run (docs/samples/opencode), translated, each as a generic map.
func translated(t *testing.T, sample string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "docs", "samples", "opencode", sample))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		for _, line := range Translate(scanner.Bytes()) {
			var m map[string]any
			if err := json.Unmarshal(line, &m); err != nil {
				t.Fatalf("a translated line isn't JSON: %s", line)
			}
			out = append(out, m)
		}
	}
	return out
}

func types(lines []map[string]any) string {
	var out []string
	for _, l := range lines {
		kind, _ := l["type"].(string)
		if msg, ok := l["message"].(map[string]any); ok {
			if content, ok := msg["content"].([]any); ok && len(content) > 0 {
				if block, ok := content[0].(map[string]any); ok {
					kind += ":" + block["type"].(string)
				}
			}
		}
		out = append(out, kind)
	}
	return strings.Join(out, " ")
}

func TestATextOnlyTurnIsTheAssistantsTextThenTheResult(t *testing.T) {
	lines := translated(t, "turn-text-only.jsonl")
	if got := types(lines); got != "assistant:text result" {
		t.Fatalf("lines: %s", got)
	}
	text := lines[0]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if text != "ok" {
		t.Errorf("text = %v", text)
	}
	if lines[0]["session_id"] != "ses_ea1314546ffeACWO3dH6hJDXvr" || lines[1]["session_id"] != "ses_ea1314546ffeACWO3dH6hJDXvr" {
		t.Errorf("the session isn't carried: %v, %v", lines[0]["session_id"], lines[1]["session_id"])
	}
	if lines[1]["is_error"] != false || lines[1]["subtype"] != "success" {
		t.Errorf("result: %v", lines[1])
	}
	if _, has := lines[1]["total_cost_usd"]; has {
		t.Errorf("a free model's turn says it cost something: %v", lines[1]["total_cost_usd"])
	}
}

// OpenCode prints a tool once, finished: the page is given the use and its result, as Claude's stream gives them.
func TestAToolTurnGivesTheUseAndItsResultAndOnlyTheLastStepEndsTheTurn(t *testing.T) {
	lines := translated(t, "turn-with-tool.jsonl")
	if got := types(lines); got != "assistant:text assistant:tool_use user:tool_result assistant:text result" {
		t.Fatalf("lines: %s", got)
	}
	use := lines[1]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	result := lines[2]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if use["name"] != "Read" {
		t.Errorf("tool name = %v", use["name"])
	}
	if input, _ := use["input"].(map[string]any); input["filePath"] != "/tmp/oc-install/home/work/note.txt" {
		t.Errorf("tool input = %v", use["input"])
	}
	if use["id"] == "" || use["id"] != result["tool_use_id"] {
		t.Errorf("use %v and result %v aren't paired", use["id"], result["tool_use_id"])
	}
	if !strings.Contains(result["content"].(string), "hello-from-file") || result["is_error"] != false {
		t.Errorf("result = %v", result)
	}
}

func TestAnErrorIsAResultThatSaysWhatWentWrong(t *testing.T) {
	lines := translated(t, "error-unknown-model.jsonl")
	if got := types(lines); got != "result" {
		t.Fatalf("lines: %s", got)
	}
	if lines[0]["is_error"] != true || !strings.Contains(lines[0]["result"].(string), "Unexpected server error") {
		t.Errorf("result: %v", lines[0])
	}
	if lines[0]["session_id"] != "ses_ea12635cfffe2Kv18etSEfcAJ4" {
		t.Errorf("session: %v", lines[0]["session_id"])
	}
}

func TestAToolThatFailedIsAnErrorResult(t *testing.T) {
	line := `{"type":"tool_use","sessionID":"ses_x","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"error","input":{"command":"false"},"error":"exited 1"}}}`
	out := Translate([]byte(line))
	if len(out) != 2 {
		t.Fatalf("lines: %s", out)
	}
	var result struct {
		Message struct {
			Content []struct {
				Content string `json:"content"`
				IsError bool   `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	json.Unmarshal(out[1], &result)
	if !result.Message.Content[0].IsError || !strings.Contains(result.Message.Content[0].Content, "exited 1") {
		t.Errorf("result: %s", out[1])
	}
}

func TestACostIsKeptWhenThereWasOne(t *testing.T) {
	line := `{"type":"step_finish","sessionID":"ses_x","part":{"type":"step-finish","reason":"stop","cost":0.0123,"tokens":{"total":10}}}`
	out := Translate([]byte(line))
	var m map[string]any
	json.Unmarshal(out[0], &m)
	if m["total_cost_usd"] != 0.0123 {
		t.Errorf("result: %s", out[0])
	}
}

func TestWhatIsntKnownGoesThroughAsItIs(t *testing.T) {
	for _, line := range []string{`not json`, `{"type":"something_new","sessionID":"ses_x"}`} {
		out := Translate([]byte(line))
		if len(out) != 1 || string(out[0]) != line {
			t.Errorf("%q became %q", line, out)
		}
	}
}

func TestAStepFinishThatIsntTheLastIsNothing(t *testing.T) {
	line := `{"type":"step_finish","sessionID":"ses_x","part":{"type":"step-finish","reason":"tool-calls"}}`
	if out := Translate([]byte(line)); len(out) != 0 {
		t.Errorf("lines: %s", out)
	}
}
