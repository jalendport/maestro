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
	if c.Harness == "grok" {
		return nil
	}
	lane := os.Getenv("MAESTRO_TASK_FOLDER") != "" || os.Getenv("MAESTRO_LANE") != ""
	if !lane && (c.Harness == "claude" || c.Harness == "codex") {
		lane = lanePointerSentence.MatchString(c.Prompt)
	}
	if !lane {
		var err error
		_, lane, err = FindLaneFolder(c)
		if err != nil {
			if c.Harness == "agy" {
				return []byte(`{}`)
			}
			return nil
		}
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
