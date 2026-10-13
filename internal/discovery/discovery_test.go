package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jalendport/maestro/internal/harness"
)

func TestScanRecordsAgySettings(t *testing.T) {
	for _, absent := range []bool{true, false} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if !absent {
			path := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["command(rm)","read_file(*)"]}}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		bins := map[string]string{"claude": "/not-installed/claude", "codex": "/not-installed/codex", "grok": "/not-installed/grok", "agy": "/not-installed/agy"}
		cache := Scan(harness.New(bins))
		data, err := json.Marshal(cache)
		if err != nil {
			t.Fatal(err)
		}
		var restored Cache
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		s := restored.Harness["agy"].Settings
		if s == nil || s.Absent != absent || s.ReadAt.IsZero() {
			t.Fatalf("settings: %+v", s)
		}
		if !absent && (len(s.Commands) != 1 || s.Commands[0] != "rm") {
			t.Fatalf("commands: %v", s.Commands)
		}
	}
}

func TestSoloPresetDiscoveryKeepsAmbiguity(t *testing.T) {
	cache := &Cache{Harness: map[string]Record{"claude": {SoloTool: 99}}}
	tools := []SoloAgentTool{
		{ID: 2, Name: "Claude B", ToolType: "claude", Enabled: true},
		{ID: 1, Name: "Claude A", ToolType: "claude", Enabled: true},
		{ID: 3, Name: "Codex", ToolType: "codex", Enabled: true},
		{ID: 4, Name: "Disabled", ToolType: "claude"},
		{ID: 5, Name: "Grok A", ToolType: "generic", Enabled: true},
		{ID: 6, Name: "Grok B", ToolType: "generic", Enabled: true},
	}
	if !cache.RecordSoloTools(tools) {
		t.Fatal("discovery not updated")
	}
	claude := cache.Harness["claude"]
	if claude.SoloTool != 0 || len(claude.SoloPresets) != 2 || claude.SoloPresets[0].ID != 1 || claude.SoloPresets[1].ID != 2 {
		t.Fatalf("ambiguous presets: %+v", claude)
	}
	if cache.Harness["codex"].SoloTool != 3 {
		t.Fatal("unique preset not selected")
	}
	if cache.Harness["grok"].SoloTool != 0 || len(cache.Harness["grok"].SoloPresets) != 2 {
		t.Fatal("generic Grok presets not detected")
	}
	if cache.RecordSoloTools(tools) {
		t.Fatal("same presets changed discovery")
	}
	if !cache.RecordSoloTools(nil) || cache.Harness["codex"].SoloTool != 0 {
		t.Fatal("removed preset retained")
	}
}
