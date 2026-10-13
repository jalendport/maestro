package budget

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
)

// ProbeClaude runs a one-word Haiku prompt and harvests its rate_limit_event.
// It costs a few thousand cached tokens; it runs only when a reading is stale
// and a Claude Lane is a candidate.
func ProbeClaude(bin string, env []string) ([]harness.Window, error) {
	if bin == "" {
		return nil, fmt.Errorf("claude is not installed")
	}
	cmd := exec.Command(bin, "-p", "--model", "haiku", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk", "--permission-prompts", "none", "--allowedTools", "Read", "--no-session-persistence")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("Reply with the single word OK.\n")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	p := (&harness.Claude{}).NewParser()
	for _, line := range strings.Split(string(out), "\n") {
		p.Line([]byte(line))
	}
	o := p.Outcome()
	if len(o.Windows) == 0 {
		return nil, fmt.Errorf("no rate_limit_event in the probe output")
	}
	return o.Windows, nil
}

// CodexLimits is the app-server's rate-limit answer, reduced to what the
// Budget keeps.
type CodexLimits struct {
	Windows []harness.Window
	Plan    string
	Credits int
	Reached string
}

// ReadCodex asks `codex app-server` for account/rateLimits/read. No model turn.
func ReadCodex(bin string) (*CodexLimits, error) {
	if bin == "" {
		return nil, fmt.Errorf("codex is not installed")
	}
	cmd := exec.Command(bin, "app-server")
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
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	go func() {
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"maestro","title":"Maestro","version":"dev"}}}`)
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":2,"method":"account/rateLimits/read","params":{}}`)
	}()
	deadline := time.After(15 * time.Second)
	lines := make(chan []byte, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			lines <- append([]byte(nil), sc.Bytes()...)
		}
		close(lines)
	}()
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("codex app-server: no answer in 15 s")
		case line, ok := <-lines:
			if !ok {
				return nil, fmt.Errorf("codex app-server ended without an answer")
			}
			var msg struct {
				ID     int `json:"id"`
				Result struct {
					RateLimits struct {
						PlanType string  `json:"planType"`
						Reached  *string `json:"rateLimitReachedType"`
						Primary  *struct {
							UsedPercent float64 `json:"usedPercent"`
							WindowMins  int     `json:"windowDurationMins"`
							ResetsAt    int64   `json:"resetsAt"`
						} `json:"primary"`
						Secondary *struct {
							UsedPercent float64 `json:"usedPercent"`
							WindowMins  int     `json:"windowDurationMins"`
							ResetsAt    int64   `json:"resetsAt"`
						} `json:"secondary"`
					} `json:"rateLimits"`
					ResetCredits struct {
						Available int `json:"availableCount"`
					} `json:"rateLimitResetCredits"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(line, &msg) != nil || msg.ID != 2 {
				continue
			}
			if msg.Error != nil {
				return nil, fmt.Errorf("codex app-server: %s", msg.Error.Message)
			}
			cl := &CodexLimits{Plan: msg.Result.RateLimits.PlanType, Credits: msg.Result.ResetCredits.Available}
			if msg.Result.RateLimits.Reached != nil {
				cl.Reached = *msg.Result.RateLimits.Reached
			}
			for _, w := range []*struct {
				UsedPercent float64 `json:"usedPercent"`
				WindowMins  int     `json:"windowDurationMins"`
				ResetsAt    int64   `json:"resetsAt"`
			}{msg.Result.RateLimits.Primary, msg.Result.RateLimits.Secondary} {
				if w == nil {
					continue
				}
				cl.Windows = append(cl.Windows, harness.Window{Kind: kindOfMinutes(w.WindowMins), Duration: time.Duration(w.WindowMins) * time.Minute, Used: w.UsedPercent / 100, ResetsAt: time.Unix(w.ResetsAt, 0)})
			}
			return cl, nil
		}
	}
}

// ConsumeCodexReset spends one Codex reset credit through the app-server.
func ConsumeCodexReset(bin string) error {
	cmd := exec.Command(bin, "app-server")
	stdin, _ := cmd.StdinPipe()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	go func() {
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"maestro","title":"Maestro","version":"dev"}}}`)
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":2,"method":"account/rateLimitResetCredit/consume","params":{}}`)
	}()
	sc := bufio.NewScanner(out)
	deadline := time.Now().Add(15 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		var msg struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID != 2 {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("codex app-server: %s", msg.Error.Message)
		}
		return nil
	}
	return fmt.Errorf("codex app-server: no answer")
}

func kindOfMinutes(mins int) string {
	switch {
	case mins >= 7*24*60:
		return "weekly"
	case mins > 0:
		return "session"
	}
	return "window"
}

// FindCodexRollout finds the newest session started by a tab in this workspace.
// Modification times are insufficient: an older live session can still write.
func FindCodexRollout(started time.Time, root string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(config.CodexHome(home), "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", err
	}
	var newest time.Time
	var threadID string
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		var meta struct {
			Type    string `json:"type"`
			Payload struct {
				ID        string    `json:"id"`
				Timestamp time.Time `json:"timestamp"`
				Cwd       string    `json:"cwd"`
			} `json:"payload"`
		}
		if sc.Scan() {
			_ = json.Unmarshal(sc.Bytes(), &meta)
		}
		_ = f.Close()
		if meta.Type != "session_meta" || meta.Payload.ID == "" || meta.Payload.Timestamp.Before(started) {
			continue
		}
		if filepath.Clean(meta.Payload.Cwd) != filepath.Clean(root) {
			continue
		}
		if meta.Payload.Timestamp.After(newest) {
			newest = meta.Payload.Timestamp
			threadID = meta.Payload.ID
		}
	}
	if threadID == "" {
		return "", fmt.Errorf("no Codex session started after %s in %s", started.Format(time.RFC3339Nano), root)
	}
	return threadID, nil
}

// HarvestCodexRollout reads the newest rate_limits snapshot of a Codex thread
// from its rollout file under ~/.codex/sessions.
func HarvestCodexRollout(threadID string) ([]harness.Window, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(config.CodexHome(home), "sessions", "*", "*", "*", "rollout-*"+threadID+".jsonl"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no rollout file for thread %s", threadID)
	}
	sort.Strings(matches)
	b, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		return nil, err
	}
	var windows []harness.Window
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, `"rate_limits"`) {
			continue
		}
		var ev struct {
			Payload struct {
				RateLimits *struct {
					Primary *struct {
						UsedPercent float64 `json:"used_percent"`
						WindowMins  int     `json:"window_minutes"`
						ResetsAt    int64   `json:"resets_at"`
					} `json:"primary"`
					Secondary *struct {
						UsedPercent float64 `json:"used_percent"`
						WindowMins  int     `json:"window_minutes"`
						ResetsAt    int64   `json:"resets_at"`
					} `json:"secondary"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Payload.RateLimits == nil {
			continue
		}
		windows = nil
		for _, w := range []*struct {
			UsedPercent float64 `json:"used_percent"`
			WindowMins  int     `json:"window_minutes"`
			ResetsAt    int64   `json:"resets_at"`
		}{ev.Payload.RateLimits.Primary, ev.Payload.RateLimits.Secondary} {
			if w != nil {
				windows = append(windows, harness.Window{Kind: kindOfMinutes(w.WindowMins), Duration: time.Duration(w.WindowMins) * time.Minute, Used: w.UsedPercent / 100, ResetsAt: time.Unix(w.ResetsAt, 0)})
			}
		}
	}
	if len(windows) == 0 {
		return nil, fmt.Errorf("no rate_limits snapshot in the rollout")
	}
	return windows, nil
}

// StatuslineWindows reads Claude's statusline JSON (the optional tee).
func StatuslineWindows(r io.Reader) ([]harness.Window, error) {
	var doc struct {
		RateLimits map[string]struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       int64   `json:"resets_at"`
		} `json:"rate_limits"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, err
	}
	var out []harness.Window
	for k, w := range doc.RateLimits {
		kind := map[string]string{"five_hour": "session", "seven_day": "weekly"}[k]
		if kind == "" {
			kind = k
		}
		out = append(out, harness.Window{Kind: kind, Used: w.UsedPercentage / 100, ResetsAt: time.Unix(w.ResetsAt, 0)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no rate_limits in the statusline JSON")
	}
	return out, nil
}
