package discovery

import (
	"testing"
)

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
