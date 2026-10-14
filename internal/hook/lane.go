package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

var lanePointer = regexp.MustCompile("([^\\s`\"']+/\\.agents/maestro/(?:runs/[^/\\s`\"']+|councils/[^/\\s`\"']+(?:/round-[0-9]+)?)/[^/\\s`\"']+)/brief\\.md")
var lanePointerSentence = regexp.MustCompile("Read the file `" + lanePointer.String() + "` in full, then follow it\\.")

func FindLaneFolder(c *Call) (string, bool) {
	if folder := os.Getenv("MAESTRO_TASK_FOLDER"); folder != "" {
		return folder, true
	}
	if c.TranscriptPath == "" {
		return "", false
	}
	f, err := os.Open(c.TranscriptPath)
	if err != nil {
		return "", false
	}
	defer f.Close()
	pointer := lanePointer
	if c.Harness == "claude" || c.Harness == "codex" {
		pointer = lanePointerSentence
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 16*1024*1024))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if match := pointer.FindSubmatch(scanner.Bytes()); len(match) > 0 {
			return string(match[1]), true
		}
	}
	return "", false
}

func LoadLane(folder string) (*Lane, bool) {
	b, err := os.ReadFile(filepath.Join(folder, "lane.json"))
	if err != nil {
		return nil, false
	}
	var lane *Lane
	if err := json.Unmarshal(b, &lane); err != nil || lane == nil || lane.Access != "read" && lane.Access != "edit" {
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
		if reason := unsafeShell(command); reason != "" {
			return Verdict{Kind: "deny", Reason: reason}
		}
		cwd, _ := c.Input["Cwd"].(string)
		cwd = nonEmpty(cwd, nonEmpty(c.CWD, lane.Root))
		allowedCwd := false
		for _, dir := range append([]string{lane.Root, lane.RunFolder}, lane.Dirs...) {
			if inside(cwd, dir) {
				allowedCwd = true
				break
			}
		}
		if !allowedCwd {
			return Verdict{Kind: "deny", Reason: "outside the Lane's working directories: " + cwd}
		}
		for _, segment := range commandSegments(command) {
			if flag := unsafeReadFlag(segment); flag != "" {
				return Verdict{Kind: "deny", Reason: "read-only command carries a write flag: " + flag}
			}
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
	abs, err := resolvePath(dir)
	if err != nil {
		return false
	}
	path, err = resolvePath(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(abs, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ancestor := path
	var rest []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			for i := len(rest) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, rest[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		rest = append(rest, filepath.Base(ancestor))
		ancestor = filepath.Dir(ancestor)
	}
}

// Keep the read-only commands and their write flags together so an added command
// cannot hide its write options in a separate permission table.
var readOnlyCommands = map[string][]string{
	"cat": nil, "ls": nil, "head": nil, "tail": nil, "grep": nil, "rg": nil,
	"find": {"-exec", "-execdir", "-delete", "-fprint", "-fprintf", "-fls", "-ok", "-okdir"},
	"wc":   nil, "pwd": nil, "echo": nil, "which": nil, "date": nil, "diff": nil,
	"tree": {"-o"}, "sort": {"-o", "--output"}, "uniq": nil, "cut": nil,
	"basename": nil, "dirname": nil, "stat": nil, "realpath": nil,
	"git log": {"--output"}, "git diff": {"--output"}, "git show": {"--output"},
	"git status": nil, "git blame": nil,
	"git branch": {"-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force", "--set-upstream-to", "--unset-upstream", "--edit-description"},
}

func readCommand(words []string) (string, int) {
	if len(words) == 0 {
		return "", 0
	}
	if words[0] == "git" && len(words) > 1 {
		return "git " + words[1], 2
	}
	return words[0], 1
}

func unsafeReadFlag(command string) string {
	words := commandWords(command)
	name, start := readCommand(words)
	for _, word := range words[start:] {
		for _, flag := range readOnlyCommands[name] {
			if word == flag || strings.HasPrefix(flag, "--") && strings.HasPrefix(word, flag+"=") {
				return flag
			}
			if len(flag) == 2 && strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word[1:], flag[1:]) {
				return flag
			}
		}
	}
	return ""
}

func commandAllowed(command string, lane *Lane) bool {
	words := commandWords(command)
	if len(words) == 0 {
		return false
	}
	name, _ := readCommand(words)
	if _, ok := readOnlyCommands[name]; ok {
		return true
	}
	switch words[0] {
	case "git":
		if len(words) > 1 {
			switch words[1] {
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

func unsafeShell(command string) string {
	for _, construct := range []string{"\n", "$(", "`", "<(", ">("} {
		if strings.Contains(command, construct) {
			if construct == "\n" {
				return "shell construct is not allowed: newline"
			}
			return "shell construct is not allowed: " + construct
		}
	}
	var quote byte
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
		if ch == '>' || ch == '<' {
			construct := string(ch)
			if i+1 < len(command) && command[i+1] == ch {
				construct += string(ch)
			}
			if i > 0 && (command[i-1] == '2' || command[i-1] == '&') {
				construct = string(command[i-1]) + construct
			}
			return "shell redirect is not allowed: " + construct
		}
		if ch == '&' {
			if i+1 < len(command) && command[i+1] == '>' {
				return "shell redirect is not allowed: &>"
			}
			if i+1 < len(command) && command[i+1] == '&' {
				i++
				continue
			}
			return "shell construct is not allowed: single &"
		}
	}
	return ""
}

func commandWords(command string) []string {
	var words []string
	var word strings.Builder
	var quote byte
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\\' && quote != '\'' && i+1 < len(command) {
			i++
			word.WriteByte(command[i])
		} else if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
		} else if ch == '\'' || ch == '"' {
			quote = ch
		} else if ch == ' ' || ch == '\t' {
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
		} else {
			word.WriteByte(ch)
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
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
