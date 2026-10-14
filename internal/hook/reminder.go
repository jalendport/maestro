package hook

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/jalendport/maestro/internal/adapter"
)

func isReminderEvent(c *Call, raw map[string]any) bool {
	if _, ok := raw["toolCall"]; ok {
		return false
	}
	switch c.Harness {
	case "claude", "codex":
		return raw["hook_event_name"] == "UserPromptSubmit"
	case "agy":
		return true
	}
	return false
}

// RenderReminder writes the Reminder in the Harness's shape. An empty scope
// keeps a Reminder event out of scope when the Roster cannot load.
func RenderReminder(c *Call, scope string) []byte {
	lane := os.Getenv("MAESTRO_TASK_FOLDER") != "" || os.Getenv("MAESTRO_LANE") != "" || lanePointerSentence.MatchString(c.Prompt)
	if !lane {
		_, lane = FindLaneFolder(c)
	}
	inScope := scope != "" && (scope == "all" || inProject(c.CWD)) && !lane
	if !inScope {
		if c.Harness == "agy" {
			return []byte(`{}`)
		}
		return nil
	}
	var out any
	switch c.Harness {
	case "claude", "codex":
		out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "UserPromptSubmit", "additionalContext": adapter.Reminder}}
	case "agy":
		out = map[string]any{"injectSteps": []any{map[string]any{"ephemeralMessage": adapter.Reminder}}}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return bytes.TrimSpace(buf.Bytes())
}
