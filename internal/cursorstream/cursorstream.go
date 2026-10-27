// Package cursorstream reads the stream-json that Cursor's agent prints
// (cursor-agent -p --output-format stream-json) as the stream Claude prints,
// which is what the companion page draws: text as assistant lines, a tool
// as a tool_use and then its tool_result, and how the turn ended as a
// result. See docs/plan-cursor.md and docs/samples/cursor/ for what Cursor
// really says.
package cursorstream

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// maxResult bounds the text of a tool's result: the page shows a summary of
// one, and the conversation keeps every line.
const maxResult = 64 * 1024

// Translate is line, one line of Cursor's output, as the lines the page
// draws: none for what the page has no use for (Cursor's echo of the prompt,
// its thinking), one for most, and for a tool that has started or finished,
// a tool_use or tool_result. A line it doesn't know goes through as it is.
func Translate(line []byte) [][]byte {
	var head struct {
		Type      string          `json:"type"`
		Subtype   string          `json:"subtype"`
		SessionID string          `json:"session_id"`
		CallID    string          `json:"call_id"`
		ToolCall  json.RawMessage `json:"tool_call"`
	}
	if json.Unmarshal(line, &head) != nil {
		return [][]byte{line}
	}
	switch head.Type {
	case "user", "thinking":
		return nil
	case "tool_call":
		var fields map[string]json.RawMessage
		if json.Unmarshal(head.ToolCall, &fields) != nil {
			return [][]byte{line}
		}
		for key, raw := range fields {
			// The tool is the one field named for it (readToolCall); the others
			// (toolCallId, startedAtMs) are Cursor's bookkeeping.
			if !strings.HasSuffix(key, "ToolCall") {
				continue
			}
			var call struct {
				Args   json.RawMessage `json:"args"`
				Result map[string]any  `json:"result"`
			}
			if json.Unmarshal(raw, &call) != nil {
				break
			}
			switch head.Subtype {
			case "started":
				return [][]byte{toolUse(head.SessionID, head.CallID, toolName(key), call.Args)}
			case "completed":
				text, failed := resultText(call.Result)
				return [][]byte{toolResult(head.SessionID, head.CallID, text, failed)}
			}
		}
	}
	return [][]byte{line}
}

// toolName is what the page calls a tool: Cursor's key for it (readToolCall,
// shellToolCall) without the ToolCall, capitalised, as Claude's are (Read).
func toolName(key string) string {
	name := strings.TrimSuffix(key, "ToolCall")
	if name == "" {
		return key
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func toolUse(session, id, name string, args json.RawMessage) []byte {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return marshal(map[string]any{
		"type": "assistant", "session_id": session,
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": id, "name": name, "input": args},
		}},
	})
}

func toolResult(session, id, text string, failed bool) []byte {
	return marshal(map[string]any{
		"type": "user", "session_id": session,
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": id, "content": text, "is_error": failed},
		}},
	})
}

func marshal(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}

// resultFields are the fields of a tool's result worth showing, best first:
// what a shell printed, what a file held, what an edit said it did, why a
// tool failed.
var resultFields = []string{"interleavedOutput", "stdout", "content", "message", "errorMessage", "stderr", "diffString"}

// resultText is what a tool's result says, and whether it failed. Cursor
// gives a result as an object under success, failure or error (or a name not
// yet seen, which counts as a failure, the safer way to be wrong), holding
// the tool's own fields; beside it may be plain flags (isBackground).
func resultText(result map[string]any) (text string, failed bool) {
	kinds := make([]string, 0, len(result))
	for kind, v := range result {
		if _, isObject := v.(map[string]any); isObject {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return "", false
	}
	sort.Strings(kinds)
	kind := kinds[0]
	if slices.Contains(kinds, "success") {
		kind = "success"
	}
	fields := result[kind].(map[string]any)
	failed = kind != "success"
	for _, name := range resultFields {
		if s, ok := fields[name].(string); ok && s != "" {
			return clip(s), failed
		}
	}
	if len(fields) == 0 {
		return "", failed
	}
	// A file that is an image is given to the model as data, and Cursor says only how big it was and
	// where (a blob's id, not the picture), which isn't worth showing as it is.
	if _, isImage := fields["dataBlobId"]; isImage {
		size, _ := fields["fileSize"].(float64)
		path, _ := fields["path"].(string)
		return fmt.Sprintf("image, %d bytes (%s)", int64(size), path), failed
	}
	return clip(string(marshal(fields))), failed
}

func clip(s string) string {
	if len(s) > maxResult {
		return s[:maxResult] + "\n… (cut)"
	}
	return s
}
