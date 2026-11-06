package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/solo"
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
}

type Verdict struct {
	Kind   string
	Reason string
}

const FileJudgment = "hook-judgment.json"

// Judgment is evidence written by Maestro after it judges a Lane tool call.
type Judgment struct {
	Harness string    `json:"harness"`
	Tool    string    `json:"tool"`
	Verdict string    `json:"verdict"`
	At      time.Time `json:"at"`
}

func RecordJudgment(folder, runFolder string, call *Call, verdict Verdict) error {
	if call.Tool == "" {
		return nil
	}
	b, err := json.Marshal(Judgment{Harness: call.Harness, Tool: call.Tool, Verdict: verdict.Kind, At: time.Now()})
	if err != nil {
		return err
	}
	if err := writeJudgment(folder, b); err != nil {
		if runFolder == "" {
			return err
		}
		// The Grok workspace sandbox cannot write the control folder.
		if fallbackErr := writeJudgment(runFolder, b); fallbackErr != nil {
			return fmt.Errorf("control folder: %v; Run folder: %w", err, fallbackErr)
		}
	}
	return nil
}

func writeJudgment(folder string, b []byte) error {
	f, err := os.OpenFile(filepath.Join(folder, FileJudgment), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (v Verdict) verdict() Verdict { return v }

// lanePointer matches a Brief path; the prefix before `.agents/` is optional
// because a headless Lane gets a project-relative Pointer.
var lanePointer = regexp.MustCompile("((?:[^`]+/)?\\.agents/maestro/(?:runs/[^/`]+|councils/[^/`]+(?:/round-[0-9]+)?)/[^/`]+)/brief\\.md")
var lanePointerSentence = regexp.MustCompile("^\\s*Read the file `" + lanePointer.String() + "` in full, then follow it\\.")

const transcriptReadLimit = 16 * 1024 * 1024

func laneFolder(c *Call, folder string) (string, bool, error) {
	if !filepath.IsAbs(folder) {
		base := c.CWD
		if c.Harness == "agy" && len(c.WorkspacePaths) > 0 {
			base = c.WorkspacePaths[0]
		}
		folder = filepath.Join(base, folder)
	}
	folder, err := filepath.Abs(folder)
	return folder, err == nil, err
}

func FindLaneFolder(c *Call) (string, bool, error) {
	if folder := os.Getenv("MAESTRO_TASK_FOLDER"); folder != "" {
		return laneFolder(c, folder)
	}
	if c.TranscriptPath == "" {
		return "", false, nil
	}
	f, err := os.Open(c.TranscriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("cannot open Lane transcript %q: %w", c.TranscriptPath, err)
	}
	defer f.Close()
	if c.Harness != "claude" && c.Harness != "codex" && c.Harness != "grok" && c.Harness != "agy" {
		return "", false, nil
	}
	reader := &io.LimitedReader{R: f, N: transcriptReadLimit}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if c.Harness == "grok" {
		// Grok stores a prompt as chunks with one prompt index.
		var prompt strings.Builder
		first := -1
		for scanner.Scan() {
			text, index, ok := grokUserChunk(scanner.Bytes())
			if ok && (first < 0 || index == first) {
				first = index
				prompt.WriteString(text)
				if match := lanePointerSentence.FindStringSubmatch(prompt.String()); len(match) > 0 {
					return laneFolder(c, match[1])
				}
				continue
			}
			if first >= 0 {
				break
			}
		}
		if err := transcriptScanError(c, scanner, reader); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	for scanner.Scan() {
		if prompt, ok := transcriptUserPrompt(c.Harness, scanner.Bytes()); ok {
			if match := lanePointerSentence.FindStringSubmatch(prompt); len(match) > 0 {
				return laneFolder(c, match[1])
			}
			return "", false, nil
		}
	}
	return "", false, transcriptScanError(c, scanner, reader)
}

func transcriptScanError(c *Call, scanner *bufio.Scanner, reader *io.LimitedReader) error {
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("cannot read Lane transcript %q: %w", c.TranscriptPath, err)
	}
	if reader.N == 0 {
		return fmt.Errorf("Lane transcript %q reached the 16 MiB reader limit before the first user prompt was complete", c.TranscriptPath)
	}
	return nil
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
	if harness == "agy" {
		var entry struct {
			Type, Source string
			Content      json.RawMessage
		}
		if json.Unmarshal(line, &entry) != nil || entry.Type != "USER_INPUT" || entry.Source != "USER_EXPLICIT" {
			return "", false
		}
		var prompt string
		if json.Unmarshal(entry.Content, &prompt) != nil {
			return "", true
		}
		prompt = strings.TrimSpace(prompt)
		if strings.HasPrefix(prompt, "<USER_REQUEST>") {
			prompt, _, _ = strings.Cut(strings.TrimPrefix(prompt, "<USER_REQUEST>"), "</USER_REQUEST>")
		}
		return prompt, true
	}
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
	kind    string // spawn, write, command, or "" for a tool that needs a name check
	target  string // write: the file path as the tool got it
	command string // command: the command line
	cwd     string // the directory the tool call names, "" when it names none
}

// actionOf maps Harness tool names to one laneAction.
func actionOf(c *Call) laneAction {
	switch c.Tool {
	case "invoke_subagent", "define_subagent", "manage_subagents", "spawn_subagent", "task", "Task", "Agent":
		return laneAction{kind: "spawn"}
	case "write_to_file", "replace_file_content", "multi_replace_file_content":
		target, _ := c.Input["TargetFile"].(string)
		cwd, _ := c.Input["Cwd"].(string)
		return laneAction{kind: "write", target: target, cwd: cwd}
	case "write", "search_replace", "Write", "Edit", "MultiEdit", "NotebookEdit":
		target := ""
		for _, key := range []string{"file_path", "target_file", "notebook_path"} {
			if target, _ = c.Input[key].(string); target != "" {
				break
			}
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
	if c.Harness == "claude" && strings.HasPrefix(c.Tool, "mcp__solo__") {
		for _, tool := range solo.LaneTools() {
			if c.Tool == tool {
				return Verdict{Kind: "allow"}
			}
		}
		return Verdict{Kind: "deny", Reason: "outside the Claude Lane's Solo allow list: " + c.Tool}
	}
	action := actionOf(c)
	switch action.kind {
	case "spawn":
		spawn := *c
		spawn.Tool = "invoke_subagent"
		return Decide(&spawn, "all").verdict()
	case "write":
		target := action.target
		if target == "" {
			return Verdict{Kind: "deny", Reason: "write target is empty"}
		}
		if !filepath.IsAbs(target) {
			target = nonEmpty(action.cwd, lane.Root) + string(filepath.Separator) + target
		}
		if lane.Access == "edit" && insideGitDir(target, lane.Root) {
			return Verdict{Kind: "deny", Reason: "writes to the project's .git directory are not allowed"}
		}
		if inside(target, lane.RunFolder) {
			return Verdict{Kind: "auto_approve"}
		}
		if lane.Access == "read" {
			return Verdict{Kind: "deny", Reason: "read Lane: writes go to the run folder only"}
		}
		if lane.Access == "edit" {
			for _, dir := range append([]string{lane.Root}, lane.Dirs...) {
				if inside(target, dir) {
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
				return Verdict{Kind: "deny", Reason: "read-only command carries a write flag or form: " + flag}
			}
			if commandAllowed(strings.TrimSpace(segment), lane) {
				continue
			}
			_, variable := stripEnvironmentPrefix(strings.TrimSpace(segment))
			environmentReason := ""
			if variable != "" {
				environmentReason = "environment variable is not on the harmless list: " + variable + "; "
			}
			if lane.Access == "read" {
				return Verdict{Kind: "deny", Reason: environmentReason + "read Lane: command not on the read-only list; accepted commands: " + readOnlyCommandNames()}
			}
			if lane.Access == "edit" && (c.Harness == "claude" || c.Harness == "agy") && lane.Harness != "grok" && lane.Harness != "codex" {
				return Verdict{Kind: "relay", Reason: strings.TrimSuffix(environmentReason, "; ")}
			}
			return Verdict{Kind: "deny", Reason: environmentReason + "outside the Lane's allow list; add it to `checks` or the project `allow` list; accepted read-only commands: " + readOnlyCommandNames()}
		}
		return Verdict{Kind: "auto_approve"}
	default:
		// A Solo spawn inside a Lane still meets the spawn rule.
		if isSoloSpawn(c.Tool) {
			return Decide(c, "all").verdict()
		}
		if readTool(c.Tool) {
			return Verdict{Kind: "allow"}
		}
		return Verdict{Kind: "deny", Reason: "unknown Lane tool: " + c.Tool}
	}
}

func readTool(tool string) bool {
	name := strings.TrimPrefix(tool, "hashline_")
	switch name {
	case "read_file", "list_dir", "grep", "web_search", "web_fetch", "todo_write",
		"get_command_or_subagent_output", "kill_command_or_subagent", "ask_user_question",
		"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite",
		"Skill", "skill", "use_skill", "invoke_skill",
		"view_file", "grep_search", "find_by_name", "view_code_item", "search_web", "read_url_content":
		return true
	}
	for _, prefix := range []string{"solo__", "mcp__solo__"} {
		if !strings.HasPrefix(tool, prefix) {
			continue
		}
		name := strings.TrimPrefix(tool, prefix)
		for _, read := range []string{"whoami", "get_", "list_", "todo_get", "todo_list", "todo_comment_create", "scratchpad_read", "help"} {
			if strings.HasPrefix(name, read) {
				return true
			}
		}
	}
	return false
}

// insideGitDir reports a path under the project's .git directory. The name is
// compared without case: on a case-insensitive file system `.GIT/config` is the
// same file as `.git/config`, and on any other it is a directory git ignores.
func insideGitDir(path, root string) bool {
	if root == "" {
		return false
	}
	// Resolved: `.git` may be a symlink into the project, and the target the
	// path it points at. Written: the name as the caller typed it, in any case.
	if inside(path, filepath.Join(root, ".git")) {
		return true
	}
	if data, err := os.ReadFile(filepath.Join(root, ".git")); err == nil {
		if gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: "); ok {
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(root, gitdir)
			}
			if inside(path, gitdir) {
				return true
			}
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return strings.EqualFold(first, ".git")
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
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + string(filepath.Separator) + path
	}
	resolved := string(filepath.Separator)
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		switch component {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		resolved = filepath.Join(resolved, component)
		if _, err := os.Lstat(resolved); err == nil {
			var err error
			resolved, err = filepath.EvalSymlinks(resolved)
			if err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return resolved, nil
}

// Keep the read-only commands and their write flags together so an added command
// cannot hide its write options in a separate permission table.
var readOnlyCommands = map[string][]string{
	"cat": nil, "ls": nil, "head": nil, "tail": nil, "grep": nil,
	"rg":   {"--pre", "--pre-glob", "--hostname-bin"},
	"find": {"-exec", "-execdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls", "-ok", "-okdir"},
	"wc":   nil, "pwd": nil, "echo": nil, "which": nil, "date": nil, "diff": nil,
	"tree": {"-o", "-R", "-H"}, "sort": {"-o", "--output", "--compress-program"}, "uniq": nil, "cut": nil,
	"basename": nil, "dirname": nil, "stat": nil, "realpath": nil,
	"sed":  {"-i", "-I", "--in-place", "-f", "--file"},
	"file": {"-C", "--compile", "-p", "--preserve-date", "-z", "-Z", "--uncompress", "--uncompress-noreport"},
	"du":   nil, "df": nil, "tr": nil, "jq": nil,
	"yq":   {"-i", "--inplace", "--in-place", "-s", "--split-exp", "--split-exp-file"},
	"test": nil, "[": nil,
	"git log": {"--output", "--ext-diff", "--textconv"}, "git diff": {"--output", "--ext-diff", "--textconv"}, "git show": {"--output", "--ext-diff", "--textconv"},
	"git status": nil, "git blame": nil,
	"git branch":   {"-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force", "--set-upstream-to", "--unset-upstream", "--edit-description"},
	"git ls-files": nil, "git describe": {"--dirty", "--broken"}, "git rev-parse": nil,
	"git tag":    {"-a", "-d", "-f", "-s", "-m", "-F", "--annotate", "--delete", "--force", "--sign", "--message", "--file", "--local-user", "--create-reflog"},
	"go version": nil, "go env": {"-w", "-u"},
	"go list": {"-o", "-modfile", "-toolexec", "-debug-actiongraph", "-pkgdir", "-asmflags", "-gccgoflags", "-gcflags", "-ldflags"},
	"go doc":  {"-http"},
	// npm, pnpm, and Yarn are omitted: their launchers can select project code.
	"node --version": nil,
	"php --version":  nil, "composer --version": nil, "composer show": nil,
	"python3 --version": nil, "pip --version": nil, "pip list": {"--log", "--python"}, "pip show": {"--log", "--python"},
	// cargo and rustc are omitted: rustup can select a local toolchain.
	"docker --version": nil,
	"docker version":   nil, "docker info": nil, "docker ps": nil, "docker images": nil,
	"docker compose ps": nil, "docker compose config": {"-o", "--output", "--lock-image-digests"},
}

func readOnlyCommandNames() string {
	names := make([]string, 0, len(readOnlyCommands))
	for name := range readOnlyCommands {
		if name == "composer show" {
			name += " --no-plugins"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func readCommand(words []string) (string, int) {
	if len(words) == 0 {
		return "", 0
	}
	if len(words) >= 3 && words[0] == "composer" && words[1] == "--no-plugins" && words[2] == "show" {
		return "composer show", 3
	}
	for n := min(len(words), 3); n > 0; n-- {
		name := strings.Join(words[:n], " ")
		if _, ok := readOnlyCommands[name]; ok {
			return name, n
		}
	}
	if words[0] == "git" && len(words) > 1 {
		return "git " + words[1], 2
	}
	return words[0], 1
}

func unsafeReadFlag(command string) string {
	command = strings.TrimSpace(command)
	matched, variable := stripEnvironmentPrefix(command)
	if variable != "" {
		return ""
	}
	words := commandWords(matched)
	name, start := readCommand(words)
	if _, ok := readOnlyCommands[name]; ok {
		expansion := command
		if name == "[" && len(matched) > 1 && matched[0] == '[' && (matched[1] == ' ' || matched[1] == '\t') {
			expansion = command[:len(command)-len(matched)] + matched[1:]
		}
		if form := shellExpansion(expansion); form != "" {
			return "shell expansion " + form
		}
	}
	if strings.HasSuffix(name, " --version") && len(words) != start {
		return name + " with extra arguments"
	}
	if name == "composer show" {
		noPlugins := false
		for _, word := range words[1:] {
			if word == "--" {
				break
			}
			if word == "--no-plugins" {
				noPlugins = true
			}
		}
		if !noPlugins {
			return "composer show requires --no-plugins"
		}
	}
	for i, word := range words[start:] {
		// `--` ends the options of every listed command except find, whose
		// expression (-exec, -delete) comes after the paths and still runs.
		if word == "--" && name != "find" {
			break
		}
		if word == "--text" {
			continue
		}
		if name == "go list" && (word == "-mod=mod" || word == "--mod=mod" || (word == "-mod" || word == "--mod") && start+i+1 < len(words) && words[start+i+1] == "mod") {
			return "go list -mod=mod"
		}
		for _, flag := range readOnlyCommands[name] {
			option, _, _ := strings.Cut(word, "=")
			// Go accepts single and double dashes, but does not bundle flags.
			if strings.HasPrefix(name, "go ") {
				if option == flag || option == "-"+flag {
					return word
				}
				continue
			}
			if option == flag || strings.HasPrefix(flag, "--") && strings.HasPrefix(option, "--") && len(option) > 2 && strings.HasPrefix(flag, option) {
				return word
			}
			if len(flag) == 2 && strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word[1:], flag[1:]) {
				return word
			}
		}
	}
	if name == "uniq" && uniqOperands(words[start:]) > 1 {
		return "uniq with two operands"
	}
	if name == "git branch" {
		return unsafeBranchForm(words[start:])
	}
	if name == "git tag" {
		return unsafeTagForm(words[start:])
	}
	if name == "sed" {
		return unsafeSedForm(words[start:])
	}
	return ""
}

func unsafeTagForm(words []string) string {
	list := false
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case word == "-l" || word == "--list":
			list = true
		case word == "-n" || strings.HasPrefix(word, "-n") && allDigits(word[2:]):
			list = true
		case word == "--contains" || word == "--points-at" || word == "--sort":
			list = true
			if i+1 < len(words) && !strings.HasPrefix(words[i+1], "-") {
				i++
			} else if word != "--contains" {
				return "git tag " + word + " without a value"
			}
		case strings.HasPrefix(word, "--contains=") || strings.HasPrefix(word, "--points-at=") || strings.HasPrefix(word, "--sort="):
			list = true
		case word == "--":
			if !list && i+1 < len(words) {
				return "git tag operand: " + words[i+1]
			}
			return ""
		case strings.HasPrefix(word, "-"):
			return "git tag form: " + word
		default:
			if !list {
				return "git tag operand: " + word
			}
		}
	}
	return ""
}

func uniqOperands(words []string) int {
	operands := 0
	options := true
	for i := 0; i < len(words); i++ {
		word := words[i]
		if options && word == "--" {
			options = false
		} else if options && strings.HasPrefix(word, "-") && word != "-" {
			switch word {
			case "-f", "-s", "-w", "--skip-fields", "--skip-chars", "--check-chars":
				i++
			}
		} else {
			operands++
		}
	}
	return operands
}

func unsafeBranchForm(words []string) string {
	list, operands, options := false, 0, true
	for i := 0; i < len(words); i++ {
		word := words[i]
		if options {
			switch word {
			case "--":
				options = false
				continue
			case "--list":
				list = true
				continue
			case "--show-current", "-a", "-r", "-v", "-vv":
				continue
			case "--contains", "--merged", "--no-merged", "--points-at":
				if i+1 >= len(words) {
					return "git branch " + word + " without a value"
				}
				i++
				continue
			}
			option, _, value := strings.Cut(word, "=")
			if value && (option == "--contains" || option == "--merged" || option == "--no-merged" || option == "--points-at") {
				continue
			}
			if strings.HasPrefix(word, "-") {
				return "git branch form: " + word
			}
		}
		operands++
		if !list || operands > 1 {
			return "git branch operand: " + word
		}
	}
	return ""
}

func commandAllowed(command string, lane *Lane) bool {
	command, _ = stripEnvironmentPrefix(command)
	words := commandWords(command)
	if len(words) == 0 {
		return false
	}
	name, _ := readCommand(words)
	if _, ok := readOnlyCommands[name]; ok {
		return true
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

// shellExpansion names the first word of a read-only command the shell would
// execute or rewrite before the command runs: a parameter or ANSI-C form (`$`),
// a backquote, a parenthesis, or a brace. A glob needs a fixed prefix so it
// cannot match a file name that starts with a dash. In a word that starts with
// a dash, a glob must follow an equals sign.
func shellExpansion(command string) string {
	var quote byte
	prefix, dash, equals := false, false, false
	fixed := func(ch byte) {
		if !prefix {
			dash = ch == '-'
		}
		if ch == '=' {
			equals = true
		}
		prefix = true
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\\' && quote != '\'' {
			i++
			if i < len(command) {
				fixed(command[i])
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if quote == '"' && (ch == '$' || ch == '`') {
				return string(ch)
			} else {
				fixed(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '$', '`', '(', '{', '}':
			// `(` covers zsh glob qualifiers such as `file(e:'cmd':)`.
			return string(ch)
		case ' ', '\t':
			prefix, dash, equals = false, false, false
		case '*', '?', '[':
			if !prefix || dash && !equals {
				return "glob can match a file name that starts with -; write ./ before it"
			}
		default:
			fixed(ch)
		}
	}
	return ""
}

func unsafeShell(command string) string {
	for _, construct := range []string{"\n", "$(", "`", "<(", ">(", "=("} {
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
		if ch == '>' && i > 0 && command[i-1] == '2' && strings.HasPrefix(command[i:], ">&1") {
			// 2>&1 joins stderr to stdout; it names no file.
			i += 2
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

var environmentAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripEnvironmentPrefix removes env and harmless assignments. It returns the
// first other variable name so the caller can deny or Relay the command.
// Keep the remaining text as written for check and allow rule matching.
func stripEnvironmentPrefix(command string) (string, string) {
	words, ends := commandWordsWithEnds(command)
	i := 0
	for i < len(words) {
		if environmentAssignment.MatchString(words[i]) {
			name, _, _ := strings.Cut(words[i], "=")
			switch name {
			case "CI", "NO_COLOR", "FORCE_COLOR", "TERM", "COLUMNS", "LINES", "LANG", "LC_ALL", "TZ", "CGO_ENABLED", "GOOS", "GOARCH", "MAESTRO_HOME":
			default:
				return command, name
			}
			i++
			continue
		}
		if words[i] != "env" {
			break
		}
		i++
		for i < len(words) {
			if words[i] == "-i" {
				i++
			} else if words[i] == "-u" && i+1 < len(words) {
				i += 2
			} else {
				break
			}
		}
	}
	if i == 0 {
		return command, ""
	}
	return strings.TrimSpace(command[ends[i-1]:]), ""
}

func commandWords(command string) []string {
	words, _ := commandWordsWithEnds(command)
	return words
}

func commandWordsWithEnds(command string) ([]string, []int) {
	var words []string
	var ends []int
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
				ends = append(ends, i)
				word.Reset()
			}
		} else {
			word.WriteByte(ch)
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
		ends = append(ends, len(command))
	}
	return words, ends
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
