package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Tests replace this path so they do not depend on the machine's policy.
var grokManagedDir = "/etc/grok"

func enabledFalse(v *value) bool {
	flag := v.get("enabled")
	return flag != nil && flag.scalar == false
}

func hasDisabledField(v *value) bool {
	if v == nil {
		return false
	}
	if enabledFalse(v) {
		return true
	}
	for _, m := range v.object {
		// Event entries and handlers are arrays. Other object fields do not
		// control whether the Harness runs a hook.
		if m.value.kind == '[' && hasDisabledField(m.value) {
			return true
		}
	}
	for _, item := range v.array {
		if hasDisabledField(item) {
			return true
		}
	}
	return false
}

func removeDisabledFields(v *value) bool {
	if v == nil {
		return false
	}
	changed := false
	var fields []member
	for _, m := range v.object {
		if m.key == "enabled" && m.value.scalar == false {
			changed = true
			continue
		}
		if m.value.kind == '[' {
			changed = removeDisabledFields(m.value) || changed
		}
		fields = append(fields, m)
	}
	v.object = fields
	for _, item := range v.array {
		changed = removeDisabledFields(item) || changed
	}
	return changed
}

// disabledReason checks the hook controls that are available as files.
// macOS MDM and Claude's managed settings are outside these checks.
func disabledReason(harness, hookFile, home string) string {
	switch harness {
	case "agy":
		data, err := os.ReadFile(hookFile)
		if err != nil {
			return ""
		}
		root, err := decode(data)
		if err == nil && hasDisabledField(root.get("maestro")) {
			return fmt.Sprintf("enabled: false in %s disables the Maestro hook; run maestro adapter install agy", hookFile)
		}
	case "grok":
		path := filepath.Join(grokHome(home), "disabled-hooks")
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("cannot read %s: %s", path, err)
		}
		absolute, err := filepath.Abs(hookFile)
		if err != nil {
			return fmt.Sprintf("cannot resolve hook path %s: %s", hookFile, err)
		}
		// Grok documents this file but not its record format. Treat each
		// non-empty, non-comment line as a name, stem, or absolute hook path.
		base := filepath.Base(hookFile)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if line == base || line == stem || line == absolute {
				return path + " lists the Maestro hook; remove that line"
			}
		}
		for _, dir := range []string{grokHome(home), grokManagedDir} {
			for _, name := range []string{"requirements.toml", "managed_config.toml"} {
				path := filepath.Join(dir, name)
				data, err := os.ReadFile(path)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return fmt.Sprintf("cannot read %s: %s", path, err)
				}
				var settings map[string]any
				if _, err := toml.Decode(string(data), &settings); err != nil {
					return fmt.Sprintf("cannot check allow_managed_hooks_only in %s: %s", path, err)
				}
				if key := managedHooksOnly(settings); key != "" {
					return fmt.Sprintf("%s in %s skips user hooks", key, path)
				}
			}
		}
	}
	return ""
}

func managedHooksOnly(settings map[string]any) string {
	for _, key := range []string{"allow_managed_hooks_only", "allowManagedHooksOnly"} {
		if v, ok := settings[key]; ok && v != false {
			return key
		}
	}
	for _, v := range settings {
		if table, ok := v.(map[string]any); ok {
			if key := managedHooksOnly(table); key != "" {
				return key
			}
		}
	}
	return ""
}
