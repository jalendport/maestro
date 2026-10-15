package adapter

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jalendport/maestro/internal/config"
)

const Reminder = "Maestro: you are the Orchestrator. You plan, judge, and read every diff; Lanes write the code. When the user approves a plan, write the Plan file and run `maestro run`. One bounded task is a Brief file and `maestro dispatch`. A review, or one question for several minds, is `maestro council`. The maestro skill holds the rules; `maestro help brief` and `maestro help plan` print the formats. If your prompt points you at a Maestro Brief file, you are a Lane, not the Orchestrator: ignore this and follow the Brief."

func grokHome(home string) string {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".grok")
}

func ReminderFile(home string) string { return filepath.Join(grokHome(home), "rules", "maestro.md") }

func ReminderRule() string {
	path := config.UserFile()
	if os.Getenv("MAESTRO_HOME") == "" {
		path = "~/.maestro/config.toml"
	}
	return "# Maestro\n\nThis rule applies inside a project that holds `.agents/maestro.toml`, or everywhere when `[hook] scope = \"all\"` in `" + path + "`. Elsewhere, ignore it. If your prompt points you at a Maestro Brief file, you are a Lane, not the Orchestrator: ignore this rule and follow the Brief.\n\n" + Reminder + "\n"
}

func ensureReminder(path string) (string, error) {
	data, err := os.ReadFile(path)
	state := "updated"
	if errors.Is(err, os.ErrNotExist) {
		state = "created"
	} else if err != nil {
		return "error", err
	} else if string(data) == ReminderRule() {
		return "unchanged", nil
	}
	if err := atomicWrite(path, []byte(ReminderRule()), 0o644); err != nil {
		return "error", err
	}
	return state, nil
}

func reminderState(path string) string {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil || string(data) != ReminderRule() {
		return "wrong"
	}
	return "ok"
}
