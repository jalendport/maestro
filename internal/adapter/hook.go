package adapter

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func entry(spec HarnessSpec, event HookEvent) *value {
	command, timeout := event.Command, event.Timeout
	if command == "" {
		command = Command
	}
	if timeout == 0 {
		timeout = spec.Timeout
	}
	handler := object(
		member{"type", scalar("command")},
		member{"command", scalar(command)},
		member{"timeout", scalar(timeout)},
	)
	if event.Direct {
		return handler
	}
	item := object()
	if event.Matcher {
		item.set("matcher", scalar(spec.Matcher))
	}
	item.set("hooks", array(handler))
	return item
}

func hookSet(spec HarnessSpec) *value {
	set := object()
	for _, event := range spec.Events {
		set.set(event.Name, array(entry(spec, event)))
	}
	return set
}

func maestroCommand(handler *value) bool {
	command := handler.get("command")
	if command == nil {
		return false
	}
	text, ok := command.scalar.(string)
	return ok && strings.HasPrefix(text, Command)
}

func maestroIndex(entries *value, direct bool) int {
	if entries == nil || entries.kind != '[' {
		return -1
	}
	for i, item := range entries.array {
		if direct {
			if maestroCommand(item) {
				return i
			}
			continue
		}
		handlers := item.get("hooks")
		if handlers == nil || handlers.kind != '[' {
			continue
		}
		for _, handler := range handlers.array {
			if maestroCommand(handler) {
				return i
			}
		}
	}
	return -1
}

func events(root *value, harness string, create bool) (*value, error) {
	if root.kind != '{' {
		return nil, fmt.Errorf("hook file must be a JSON object")
	}
	key := "hooks"
	if harness == "agy" {
		key = "maestro"
	}
	set := root.get(key)
	if set == nil {
		if !create {
			return nil, nil
		}
		set = object()
		root.set(key, set)
	}
	if set.kind != '{' {
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
	return set, nil
}

func mergeEntry(entries *value, spec HarnessSpec, event HookEvent) string {
	desired := entry(spec, event)
	index := maestroIndex(entries, event.Direct)
	if index < 0 {
		entries.array = append(entries.array, desired)
		return "created"
	}
	item := entries.array[index]
	if event.Direct || len(item.get("hooks").array) == 1 {
		if equal(item, desired) {
			return "unchanged"
		}
		entries.array[index] = desired
		return "updated"
	}
	changed := false
	handlers := item.get("hooks")
	for i, handler := range handlers.array {
		if maestroCommand(handler) && !equal(handler, desired.get("hooks").array[0]) {
			handlers.array[i] = desired.get("hooks").array[0]
			changed = true
		}
	}
	if !changed {
		return "unchanged"
	}
	return "updated"
}

func ensureHooks(harness, path string, spec HarnessSpec) ([]string, error) {
	states := make([]string, len(spec.Events))
	for i := range states {
		states[i] = "error"
	}
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return states, err
	}
	root := object()
	mode := os.FileMode(0o644)
	if !missing {
		root, err = decode(data)
		if err != nil {
			return states, fmt.Errorf("parse JSON: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return states, err
		}
		mode = info.Mode().Perm()
	}
	set, err := events(root, harness, true)
	if err != nil {
		return states, err
	}
	for _, event := range spec.Events {
		if entries := set.get(event.Name); entries != nil && entries.kind != '[' {
			return states, fmt.Errorf("%s must be a JSON array", event.Name)
		}
	}
	changed := false
	for i, event := range spec.Events {
		entries := set.get(event.Name)
		if entries == nil {
			entries = array()
			set.set(event.Name, entries)
		}
		states[i] = mergeEntry(entries, spec, event)
		changed = changed || states[i] != "unchanged"
	}
	if !changed {
		return states, nil
	}
	output, err := encode(root, indent(data))
	if err != nil {
		return states, err
	}
	if err := atomicWrite(path, output, mode); err != nil {
		return states, err
	}
	return states, nil
}

func hookStates(harness, path string, spec HarnessSpec) []string {
	states := make([]string, len(spec.Events))
	data, err := os.ReadFile(path)
	state := "wrong"
	if errors.Is(err, os.ErrNotExist) {
		state = "missing"
	}
	for i := range states {
		states[i] = state
	}
	if err != nil {
		return states
	}
	root, err := decode(data)
	if err != nil {
		return states
	}
	set, err := events(root, harness, false)
	if err != nil {
		return states
	}
	for i, event := range spec.Events {
		states[i] = eventState(set.get(event.Name), spec, event)
	}
	return states
}

func eventState(entries *value, spec HarnessSpec, event HookEvent) string {
	if entries == nil {
		return "missing"
	}
	if entries.kind != '[' {
		return "wrong"
	}
	index := maestroIndex(entries, event.Direct)
	if index < 0 {
		return "missing"
	}
	desired := entry(spec, event)
	if equal(entries.array[index], desired) {
		return "ok"
	}
	if !event.Direct {
		if handlers := entries.array[index].get("hooks"); len(handlers.array) > 1 {
			for _, handler := range handlers.array {
				if equal(handler, desired.get("hooks").array[0]) {
					return "ok"
				}
			}
		}
	}
	return "wrong"
}
