package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
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
// keeps a Reminder event out of scope when the Roster cannot load. liveItems
// reads project state only after the prompt is in scope and outside a Lane.
func RenderReminder(c *Call, scope, skill string, liveItems func(string) string) []byte {
	if c.Harness == "grok" {
		return nil
	}
	if !ReminderInScope(c, scope) {
		if c.Harness == "agy" {
			return []byte(`{}`)
		}
		return nil
	}
	return renderReminder(c, skill, liveItems)
}

// ReminderInScope excludes Lanes and prompts outside the configured scope.
func ReminderInScope(c *Call, scope string) bool {
	if c.Harness == "grok" || scope == "" {
		return false
	}
	lane := os.Getenv("MAESTRO_TASK_FOLDER") != "" || os.Getenv("MAESTRO_LANE") != ""
	if !lane && (c.Harness == "claude" || c.Harness == "codex") {
		lane = lanePointerSentence.MatchString(c.Prompt)
	}
	if !lane {
		var err error
		_, lane, err = FindLaneFolder(c)
		if err != nil {
			return false
		}
	}
	return (scope == "all" || inProject(c.CWD)) && !lane
}

func renderReminder(c *Call, skill string, liveItems func(string) string) []byte {
	reminder := adapter.Reminder
	if home, err := os.UserHomeDir(); err == nil {
		if stale := adapter.StaleHarnesses(home, skill); len(stale) != 0 {
			reminder += "\nAdapter stale (" + strings.Join(stale, ", ") + "): run maestro adapter install"
		}
	}
	if liveItems != nil {
		cwd := c.CWD
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		root := config.FindProjectRoot(cwd)
		if root != "" {
			if items := liveItems(root); items != "" {
				reminder += "\n" + items
			}
		}
	}
	var out any
	switch c.Harness {
	case "claude", "codex":
		out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "UserPromptSubmit", "additionalContext": reminder}}
	case "agy":
		out = map[string]any{"injectSteps": []any{map[string]any{"ephemeralMessage": reminder}}}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return bytes.TrimSpace(buf.Bytes())
}
