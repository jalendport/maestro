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

// lanePointer matches a Brief path; the prefix before `.agents/` is optional
// because a headless Lane gets a project-relative Pointer.
var lanePointer = regexp.MustCompile("((?:[^\\s`\"']+/)?\\.agents/maestro/(?:runs/[^/\\s`\"']+|councils/[^/\\s`\"']+(?:/round-[0-9]+)?)/[^/\\s`\"']+)/brief\\.md")
var lanePointerSentence = regexp.MustCompile("^\\s*Read the file `" + lanePointer.String() + "` in full, then follow it\\.")

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
	if c.Harness == "agy" {
		b, err := io.ReadAll(io.LimitReader(f, 64*1024))
		if err == nil {
			if match := lanePointer.FindSubmatch(b); len(match) > 0 {
				return string(match[1]), true
			}
		}
		return "", false
	}
	if c.Harness != "claude" && c.Harness != "codex" && c.Harness != "grok" {
		return "", false
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 16*1024*1024))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if c.Harness == "grok" {
		// Grok stores a prompt as one or more user_message_chunk updates
		// that share a prompt index; the first index is the first prompt.
		var texts []string
		first := -1
		for scanner.Scan() {
			text, index, ok := grokUserChunk(scanner.Bytes())
			if ok && (first < 0 || index == first) {
				first = index
				texts = append(texts, text)
				continue
			}
			if first >= 0 {
				break
			}
		}
		if match := lanePointerSentence.FindStringSubmatch(strings.Join(texts, "")); len(match) > 0 {
			return match[1], true
		}
		return "", false
	}
	for scanner.Scan() {
		if prompt, ok := transcriptUserPrompt(c.Harness, scanner.Bytes()); ok {
			if match := lanePointerSentence.FindStringSubmatch(prompt); len(match) > 0 {
				return match[1], true
			}
			return "", false
		}
	}
	return "", false
}

// grokUserChunk reads one line of a Grok session's updates.jsonl and returns
// the text and prompt index of a user_message_chunk update.
func grokUserChunk(line []byte) (string, int, bool) {
	var entry struct {
		Params struct {
			Update struct {
				Kind    string `json:"sessionUpdate"`
				Content struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Meta struct {
					PromptIndex int `json:"promptIndex"`
				} `json:"_meta"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &entry) != nil || entry.Params.Update.Kind != "user_message_chunk" {
		return "", 0, false
	}
	return entry.Params.Update.Content.Text, entry.Params.Update.Meta.PromptIndex, true
}

func transcriptUserPrompt(harness string, line []byte) (string, bool) {
	type message struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Meta    struct {
			Kinds []string `json:"content_item_kinds"`
		} `json:"internal_chat_message_metadata_passthrough"`
	}
	var entry struct {
		Type    string  `json:"type"`
		Message message `json:"message"`
		Payload message `json:"payload"`
	}
	if json.Unmarshal(line, &entry) != nil {
		return "", false
	}
	m := entry.Message
	if harness == "claude" {
		if entry.Type != "user" || m.Role != "user" {
			return "", false
		}
	} else {
		m = entry.Payload
		if entry.Type != "response_item" || m.Type != "message" || m.Role != "user" {
			return "", false
		}
		// Codex stores injected project rules and environment context as user
		// messages; only a `user.*` kind is something the user typed.
		if len(m.Meta.Kinds) > 0 {
			contextOnly := true
			for _, kind := range m.Meta.Kinds {
				if strings.HasPrefix(kind, "user.") {
					contextOnly = false
				}
			}
			if contextOnly {
				return "", false
			}
		}
	}
	var prompt string
	if json.Unmarshal(m.Content, &prompt) == nil && string(m.Content) != "null" {
		return prompt, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return "", false
	}
	var texts []string
	for _, block := range blocks {
		if block.Type == "text" || block.Type == "input_text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n"), len(texts) > 0
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

// laneAction is the harness-neutral view of a tool call the Lane policy judges.
type laneAction struct {
	kind    string // spawn, write, command, or "" for a tool the policy lets through
	target  string // write: the file path as the tool got it
	command string // command: the command line
	cwd     string // the directory the tool call names, "" when it names none
}

// actionOf maps Antigravity and Grok tool names to one laneAction.
func actionOf(c *Call) laneAction {
	switch c.Tool {
	case "invoke_subagent", "define_subagent", "manage_subagents", "spawn_subagent", "task", "Task":
		return laneAction{kind: "spawn"}
	case "write_to_file", "replace_file_content", "multi_replace_file_content":
		target, _ := c.Input["TargetFile"].(string)
		cwd, _ := c.Input["Cwd"].(string)
		return laneAction{kind: "write", target: target, cwd: cwd}
	case "write", "search_replace", "Write", "Edit", "MultiEdit":
		target, _ := c.Input["file_path"].(string)
		if target == "" {
			target, _ = c.Input["target_file"].(string)
		}
		return laneAction{kind: "write", target: target, cwd: c.CWD}
	case "run_command":
		command, _ := c.Input["CommandLine"].(string)
		cwd, _ := c.Input["Cwd"].(string)
		return laneAction{kind: "command", command: command, cwd: cwd}
	case "run_terminal_command", "run_terminal_cmd", "Bash":
		command, _ := c.Input["command"].(string)
		cwd, _ := c.Input["cwd"].(string)
		return laneAction{kind: "command", command: command, cwd: nonEmpty(cwd, c.CWD)}
	}
	return laneAction{}
}

func DecideLane(c *Call, lane *Lane) Verdict {
	action := actionOf(c)
	switch action.kind {
	case "spawn":
		spawn := *c
		spawn.Tool = "invoke_subagent"
		return Decide(&spawn, "all").verdict()
	case "write":
		target := action.target
		if !filepath.IsAbs(target) {
			target = filepath.Join(nonEmpty(action.cwd, lane.Root), target)
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
	case "command":
		command := action.command
		if reason := unsafeShell(command); reason != "" {
			return Verdict{Kind: "deny", Reason: reason}
		}
		cwd := nonEmpty(action.cwd, nonEmpty(c.CWD, lane.Root))
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
		// A Solo spawn inside a Lane still meets the spawn rule.
		if isSoloSpawn(c.Tool) {
			return Decide(c, "all").verdict()
		}
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
