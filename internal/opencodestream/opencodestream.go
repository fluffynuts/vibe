// Package opencodestream reads the JSON events that OpenCode prints (opencode run --format json) as the stream
// Claude prints, which is what the companion page draws: text as assistant lines, a tool as a tool_use and its
// tool_result, and the end of the turn as a result. See docs/plan-conversation-other-agents.md and
// docs/samples/opencode/ for what OpenCode really says.
package opencodestream

import (
	"encoding/json"
	"strings"
)

// maxResult bounds the text of a tool's result: the page shows a summary of one, and the conversation keeps every
// line.
const maxResult = 64 * 1024

// Translate is line, one line of OpenCode's output, as the lines the page draws: none for what the page has no use
// for (a step starting, a step that only leads to a tool), one for most, two for a tool (the use and its result,
// since OpenCode prints a tool once, finished). A line it doesn't know goes through as it is.
func Translate(line []byte) [][]byte {
	var head struct {
		Type      string `json:"type"`
		SessionID string `json:"sessionID"`
		Part      struct {
			Type   string  `json:"type"`
			Text   string  `json:"text"`
			Reason string  `json:"reason"`
			Cost   float64 `json:"cost"`
			Tool   string  `json:"tool"`
			CallID string  `json:"callID"`
			State  struct {
				Status string          `json:"status"`
				Input  json.RawMessage `json:"input"`
				Output string          `json:"output"`
				Error  string          `json:"error"`
			} `json:"state"`
		} `json:"part"`
		Error struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &head) != nil {
		return [][]byte{line}
	}
	switch head.Type {
	case "step_start":
		return nil
	case "text":
		return [][]byte{assistantText(head.SessionID, head.Part.Text)}
	case "tool_use":
		state := head.Part.State
		text, failed := state.Output, state.Status != "completed"
		if failed && state.Error != "" {
			text = state.Error
		}
		return [][]byte{
			toolUse(head.SessionID, head.Part.CallID, toolName(head.Part.Tool), state.Input),
			toolResult(head.SessionID, head.Part.CallID, clip(text), failed),
		}
	case "step_finish":
		// A step that ends in tool calls is followed by another; only the one that stops ends the turn.
		if head.Part.Reason != "stop" {
			return nil
		}
		result := map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": head.SessionID}
		if head.Part.Cost > 0 {
			result["total_cost_usd"] = head.Part.Cost
		}
		return [][]byte{marshal(result)}
	case "error":
		message := head.Error.Data.Message
		if message == "" {
			message = head.Error.Name
		}
		return [][]byte{marshal(map[string]any{
			"type": "result", "subtype": "error", "is_error": true, "session_id": head.SessionID, "result": message,
		})}
	}
	return [][]byte{line}
}

// toolName is what the page calls a tool: OpenCode's name for it (read, bash) capitalised, as Claude's are (Read).
func toolName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func assistantText(session, text string) []byte {
	return marshal(map[string]any{
		"type": "assistant", "session_id": session,
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": text},
		}},
	})
}

func toolUse(session, id, name string, input json.RawMessage) []byte {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	return marshal(map[string]any{
		"type": "assistant", "session_id": session,
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": id, "name": name, "input": input},
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

func clip(s string) string {
	if len(s) > maxResult {
		return s[:maxResult] + "\n… (cut)"
	}
	return s
}
