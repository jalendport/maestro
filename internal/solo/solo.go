// Package solo is Maestro's client for Solo: a JSON-RPC MCP session over the
// stdio helper that ships inside Solo.app. Solo identifies the session by
// process ancestry, so a Maestro started from an Orchestrator's tab acts as
// that tab; a detached Maestro registers as an external actor.
package solo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

// Helper is the stdio MCP helper inside Solo.app.
const Helper = "/Applications/Solo.app/Contents/MacOS/mcp"

func helperPath() (string, error) {
	if override := os.Getenv("MAESTRO_SOLO_MCP"); override != "" {
		return config.ExpandHome(override), nil
	}
	paths := []string{Helper}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Applications", "Solo.app", "Contents", "MacOS", "mcp"))
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", ErrAbsent
}

// ErrAbsent means Solo is not installed or this process is not inside Solo.
var ErrAbsent = errors.New("solo is not available")

// InSolo reports whether this process runs inside a Solo tab.
func InSolo() bool { return os.Getenv("SOLO_PROCESS_ID") != "" }

// ProcessID is the Solo process id of this tab, 0 outside Solo.
func ProcessID() int {
	n, _ := strconv.Atoi(os.Getenv("SOLO_PROCESS_ID"))
	return n
}

// Client is one MCP session.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	nextID int
	reply  map[int]chan json.RawMessage
	errs   map[int]chan *rpcError
	done   chan struct{}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Connect starts the helper and initializes the session.
func Connect() (*Client, error) {
	helper, err := helperPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(helper)
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: stdin, reply: map[int]chan json.RawMessage{}, errs: map[int]chan *rpcError{}, done: make(chan struct{})}
	go c.read(stdout)
	if _, err := c.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "maestro", "version": "dev"}}, 10*time.Second); err != nil {
		c.Close()
		return nil, err
	}
	c.notify("notifications/initialized", map[string]any{})
	return c, nil
}

// Close ends the session.
func (c *Client) Close() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.stdin.Close()
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
}

func (c *Client) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID == nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.reply[*msg.ID]
		ech := c.errs[*msg.ID]
		c.mu.Unlock()
		if !ok {
			continue
		}
		if msg.Error != nil {
			ech <- msg.Error
		} else {
			ch <- msg.Result
		}
	}
	close(c.done)
}

func (c *Client) notify(method string, params any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	c.mu.Lock()
	_, _ = c.stdin.Write(append(b, '\n'))
	c.mu.Unlock()
}

func (c *Client) request(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	ech := make(chan *rpcError, 1)
	c.reply[id] = ch
	c.errs[id] = ech
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	_, err := c.stdin.Write(append(b, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	defer func() {
		c.mu.Lock()
		delete(c.reply, id)
		delete(c.errs, id)
		c.mu.Unlock()
	}()
	select {
	case res := <-ch:
		return res, nil
	case e := <-ech:
		return nil, fmt.Errorf("solo %s: %s", method, e.Message)
	case <-c.done:
		return nil, fmt.Errorf("solo helper ended")
	case <-time.After(timeout):
		return nil, fmt.Errorf("solo %s: no answer in %s", method, timeout)
	}
}

// Call invokes one Solo tool and returns the JSON its text content holds.
func (c *Client) Call(tool string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := c.request("tools/call", map[string]any{"name": tool, "arguments": args}, 60*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	var text string
	for _, ct := range out.Content {
		if ct.Type == "text" {
			text += ct.Text
		}
	}
	if out.IsError {
		return nil, fmt.Errorf("solo %s: %s", tool, text)
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text), nil
	}
	// Some tools append guidance text after the JSON object: keep the object.
	if i := strings.IndexAny(text, "{["); i >= 0 {
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var raw json.RawMessage
		if dec.Decode(&raw) == nil {
			return raw, nil
		}
	}
	b, _ := json.Marshal(text)
	return json.RawMessage(b), nil
}

// Identity is what whoami reports.
type Identity struct {
	ProcessID int `json:"process_id"`
	ProjectID int `json:"project_id"`
	Effective int `json:"effective_project_id"`
}

// WhoAmI identifies the session, registering an external actor when the
// helper cannot see a Solo parent.
func (c *Client) WhoAmI() (*Identity, error) {
	res, err := c.Call("whoami", nil)
	if err != nil {
		_, _ = c.Call("identify_session", map[string]any{"external": map[string]any{"name": "maestro", "agent_id": "maestro"}})
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(res, &id); err != nil {
		return nil, err
	}
	if id.ProjectID == 0 {
		id.ProjectID = id.Effective
	}
	return &id, nil
}

// Comment posts a todo comment.
func (c *Client) Comment(projectID, todoID int, body string) error {
	_, err := c.Call("todo_comment_create", map[string]any{"project_id": projectID, "todo_id": todoID, "body": body})
	return err
}

// CreateTodo creates a todo and returns its id.
func (c *Client) CreateTodo(projectID int, title, body string, tags []string) (int, error) {
	res, err := c.Call("todo_create", map[string]any{"project_id": projectID, "title": title, "body": body, "tags": tags, "priority": "medium"})
	if err != nil {
		return 0, err
	}
	var out struct {
		TodoID int `json:"todo_id"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return 0, err
	}
	return out.TodoID, nil
}

// CompleteTodo marks a todo complete.
func (c *Client) CompleteTodo(projectID, todoID int) error {
	_, err := c.Call("todo_complete", map[string]any{"project_id": projectID, "todo_id": todoID, "completed": true})
	return err
}

// Timer types one line into an agent process after a delay.
func (c *Client) Timer(projectID, deliveryProcess int, delay time.Duration, body string) error {
	args := map[string]any{"project_id": projectID, "delay_ms": delay.Milliseconds(), "body": body}
	if deliveryProcess > 0 {
		args["delivery_process_id"] = deliveryProcess
	}
	_, err := c.Call("timer_set", args)
	return err
}

// Spawn starts an agent tab from a preset with extra arguments.
func (c *Client) Spawn(projectID, toolID int, name string, extraArgs []string) (int, error) {
	res, err := c.Call("spawn_agent", map[string]any{"project_id": projectID, "agent_tool_id": toolID, "name": name, "extra_args": extraArgs})
	if err != nil {
		return 0, err
	}
	var out struct {
		ProcessID int `json:"process_id"`
		Process   struct {
			ID int `json:"id"`
		} `json:"process"`
	}
	_ = json.Unmarshal(res, &out)
	if out.ProcessID == 0 {
		out.ProcessID = out.Process.ID
	}
	if out.ProcessID == 0 {
		return 0, fmt.Errorf("spawn_agent returned no process id: %s", string(res))
	}
	return out.ProcessID, nil
}

// SendInput types text into a process.
func (c *Client) SendInput(processID int, text string) error {
	_, err := c.Call("send_input", map[string]any{"process_id": processID, "input": text})
	return err
}

// ProcessState is what get_process_status reports that Maestro reads.
type ProcessState struct {
	Status      string  `json:"status"` // Running | Exited | ...
	Idle        bool    `json:"idle"`
	IdleSeconds float64 `json:"idle_seconds"`
	Uptime      float64 `json:"uptime_seconds"`
}

// Status returns a process's status and Solo's own idle reading.
func (c *Client) Status(processID int) (ProcessState, error) {
	res, err := c.Call("get_process_status", map[string]any{"process_id": processID})
	if err != nil {
		return ProcessState{}, err
	}
	var out struct {
		Status     string  `json:"status"`
		Uptime     float64 `json:"uptime_seconds"`
		AgentState struct {
			Idle        bool    `json:"idle"`
			IdleSeconds float64 `json:"idle_seconds"`
		} `json:"agent_state"`
	}
	_ = json.Unmarshal(res, &out)
	return ProcessState{Status: out.Status, Idle: out.AgentState.Idle, IdleSeconds: out.AgentState.IdleSeconds, Uptime: out.Uptime}, nil
}

// SendKeys sends raw PTY bytes (Enter, Esc, arrows).
func (c *Client) SendKeys(processID int, keys []byte) error {
	ints := make([]int, len(keys))
	for i, b := range keys {
		ints[i] = int(b)
	}
	_, err := c.Call("send_input", map[string]any{"process_id": processID, "bytes": ints})
	return err
}

// Running reports whether a status word means the process is alive.
func (s ProcessState) Running() bool {
	return strings.EqualFold(s.Status, "running")
}

// Output returns the last lines of a process's output.
func (c *Client) Output(processID, lines int) (string, error) {
	res, err := c.Call("get_process_output", map[string]any{"process_id": processID, "lines": lines})
	if err != nil {
		return "", err
	}
	var out struct {
		Output *string  `json:"output"`
		Lines  []string `json:"lines"`
	}
	if json.Unmarshal(res, &out) == nil && out.Output != nil {
		return *out.Output, nil
	}
	var s string
	if json.Unmarshal(res, &s) == nil {
		return s, nil
	}
	return string(res), nil
}

// CloseProcess closes a tab.
func (c *Client) CloseProcess(processID int) error {
	_, err := c.Call("close_process", map[string]any{"process_id": processID})
	return err
}
