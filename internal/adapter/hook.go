package adapter

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func entry(spec HarnessSpec) *value {
	return object(
		member{"matcher", scalar(spec.Matcher)},
		member{"hooks", array(object(
			member{"type", scalar("command")},
			member{"command", scalar(Command)},
			member{"timeout", scalar(spec.Timeout)},
		))},
	)
}

func hookSet(spec HarnessSpec) *value {
	return object(member{"PreToolUse", array(entry(spec))})
}

func maestroIndex(entries *value) int {
	if entries == nil || entries.kind != '[' {
		return -1
	}
	for i, item := range entries.array {
		handlers := item.get("hooks")
		if handlers == nil || handlers.kind != '[' {
			continue
		}
		for _, handler := range handlers.array {
			command := handler.get("command")
			if command == nil {
				continue
			}
			text, ok := command.scalar.(string)
			if ok && strings.HasPrefix(text, Command) {
				return i
			}
		}
	}
	return -1
}

func preToolUse(root *value) (*value, error) {
	if root.kind != '{' {
		return nil, fmt.Errorf("hook file must be a JSON object")
	}
	hooks := root.get("hooks")
	if hooks == nil {
		hooks = object()
		root.set("hooks", hooks)
	}
	if hooks.kind != '{' {
		return nil, fmt.Errorf("hooks must be a JSON object")
	}
	entries := hooks.get("PreToolUse")
	if entries == nil {
		entries = array()
		hooks.set("PreToolUse", entries)
	}
	if entries.kind != '[' {
		return nil, fmt.Errorf("hooks.PreToolUse must be a JSON array")
	}
	return entries, nil
}

func mergeHook(harness string, root *value, spec HarnessSpec) (*value, string, error) {
	switch harness {
	case "grok":
		desired := object(member{"hooks", hookSet(spec)})
		if equal(root, desired) {
			return root, "unchanged", nil
		}
		return desired, "updated", nil
	case "agy":
		if root.kind != '{' {
			return nil, "error", fmt.Errorf("hook file must be a JSON object")
		}
		old := root.get("maestro")
		desired := hookSet(spec)
		if equal(old, desired) {
			return root, "unchanged", nil
		}
		root.set("maestro", desired)
		if old == nil {
			return root, "created", nil
		}
		return root, "updated", nil
	default:
		entries, err := preToolUse(root)
		if err != nil {
			return nil, "error", err
		}
		desired := entry(spec)
		index := maestroIndex(entries)
		if index < 0 {
			entries.array = append(entries.array, desired)
			return root, "created", nil
		}
		if equal(entries.array[index], desired) {
			return root, "unchanged", nil
		}
		entries.array[index] = desired
		return root, "updated", nil
	}
}

func ensureHook(harness, path string, spec HarnessSpec) (string, error) {
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return "error", err
	}
	root := object()
	mode := os.FileMode(0o644)
	if !missing {
		root, err = decode(data)
		if err != nil {
			return "error", fmt.Errorf("parse JSON: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "error", err
		}
		mode = info.Mode().Perm()
	}
	root, state, err := mergeHook(harness, root, spec)
	if err != nil {
		return "error", err
	}
	if state == "unchanged" {
		return state, nil
	}
	if missing {
		state = "created"
	}
	output, err := encode(root, indent(data))
	if err != nil {
		return "error", err
	}
	if err := atomicWrite(path, output, mode); err != nil {
		return "error", err
	}
	return state, nil
}

func hookState(harness, path string, spec HarnessSpec) string {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "wrong"
	}
	root, err := decode(data)
	if err != nil {
		return "wrong"
	}
	if root.kind != '{' {
		return "wrong"
	}
	hooks := root.get("hooks")
	if harness == "agy" {
		hooks = root.get("maestro")
	}
	if hooks == nil {
		return "missing"
	}
	if hooks.kind != '{' {
		return "wrong"
	}
	entries := hooks.get("PreToolUse")
	if entries == nil {
		return "missing"
	}
	if entries.kind != '[' {
		return "wrong"
	}
	index := maestroIndex(entries)
	if index < 0 {
		return "missing"
	}
	if equal(entries.array[index], entry(spec)) {
		return "ok"
	}
	return "wrong"
}
