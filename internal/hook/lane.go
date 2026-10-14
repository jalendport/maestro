package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Lane struct {
	Task      string   `json:"task"`
	Harness   string   `json:"harness"`
	Access    string   `json:"access"`
	Root      string   `json:"root"`
	Dirs      []string `json:"dirs"`
	RunFolder string   `json:"run_folder"`
	Allow     []string `json:"allow"`
	Checks    []string `json:"checks"`
	Commit    bool     `json:"commit"`
	Tab       bool     `json:"tab"`
}

type Verdict struct {
	Kind   string
	Reason string
}

func (v Verdict) verdict() Verdict { return v }

func LoadLane(folder string) (*Lane, bool) {
	b, err := os.ReadFile(filepath.Join(folder, "lane.json"))
	if err != nil {
		return nil, false
	}
	var lane *Lane
	if err := json.Unmarshal(b, &lane); err != nil || lane == nil {
		return nil, false
	}
	return lane, true
}

func DecideLane(c *Call, lane *Lane) Verdict {
	switch c.Tool {
	case "invoke_subagent", "define_subagent", "manage_subagents":
		spawn := *c
		spawn.Tool = "invoke_subagent"
		return Decide(&spawn, "all").verdict()
	case "write_to_file", "replace_file_content", "multi_replace_file_content":
		target, _ := c.Input["TargetFile"].(string)
		cwd, _ := c.Input["Cwd"].(string)
		if !filepath.IsAbs(target) {
			target = filepath.Join(nonEmpty(cwd, lane.Root), target)
		}
		path, err := filepath.Abs(target)
		if err == nil && inside(path, lane.RunFolder) {
			return Verdict{Kind: "auto_approve"}
		}
		if lane.Access == "read" {
			return Verdict{Kind: "deny", Reason: "read Lane: writes go to the run folder only"}
		}
		if err == nil && lane.Access == "edit" {
			for _, dir := range append([]string{lane.Root}, lane.Dirs...) {
				if inside(path, dir) {
					return Verdict{Kind: "auto_approve"}
				}
			}
		}
		return Verdict{Kind: "deny", Reason: fmt.Sprintf("outside the Lane's writable directories: %s", target)}
	case "run_command":
		command, _ := c.Input["CommandLine"].(string)
		for _, segment := range commandSegments(command) {
			if commandAllowed(strings.TrimSpace(segment), lane) {
				continue
			}
			if lane.Access == "read" {
				return Verdict{Kind: "deny", Reason: "read Lane: command not on the read-only list"}
			}
			if lane.Access == "edit" && lane.Tab {
				return Verdict{Kind: "relay"}
			}
			return Verdict{Kind: "deny", Reason: "outside the Lane's allow list; add it to `checks` or the project `allow` list"}
		}
		return Verdict{Kind: "auto_approve"}
	default:
		return Verdict{Kind: "allow"}
	}
}

func inside(path, dir string) bool {
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(abs, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func commandAllowed(command string, lane *Lane) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "cat", "ls", "head", "tail", "grep", "rg", "find", "wc", "pwd", "echo", "which", "env", "date", "diff", "tree", "sort", "uniq", "cut", "basename", "dirname", "file", "stat", "realpath":
		return true
	case "git":
		if len(words) > 1 {
			switch words[1] {
			case "log", "diff", "show", "status", "branch", "blame":
				return true
			case "add", "commit":
				if lane.Access == "edit" && lane.Commit {
					return true
				}
			}
		}
	}
	if lane.Access != "edit" {
		return false
	}
	for _, check := range lane.Checks {
		if check != "" && (command == check || strings.HasPrefix(command, check+" ")) {
			return true
		}
	}
	for _, rule := range lane.Allow {
		if !strings.HasPrefix(rule, "Bash(") || !strings.HasSuffix(rule, ")") {
			continue
		}
		pattern := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
		if strings.HasSuffix(pattern, " *") {
			if strings.HasPrefix(command, strings.TrimSuffix(pattern, "*")) {
				return true
			}
		} else if pattern != "" && command == pattern {
			return true
		}
	}
	return false
}

func commandSegments(command string) []string {
	var segments []string
	var quote byte
	start := 0
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\\' && quote != '\'' {
			i++
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == ';' || ch == '|' || ch == '&' && i+1 < len(command) && command[i+1] == '&' {
			segments = append(segments, command[start:i])
			if (ch == '|' || ch == '&') && i+1 < len(command) && command[i+1] == ch {
				i++
			}
			start = i + 1
		}
	}
	return append(segments, command[start:])
}
