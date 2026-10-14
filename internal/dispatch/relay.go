package dispatch

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jalendport/maestro/internal/ui"
)

var relayDeadline = 10 * time.Minute
var relayPollInterval = time.Second

type RelayRequest struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id"`
	AskedAt   time.Time       `json:"asked_at"`
}

type relayAnswer struct {
	Request    RelayRequest `json:"request"`
	Answer     string       `json:"answer"`
	AnsweredAt time.Time    `json:"answered_at"`
}

func writeRelayJSON(folder, name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(folder, ".relay-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(folder, name))
}

func Relay(folder string, input io.Reader, output io.Writer) error {
	var request RelayRequest
	if err := json.NewDecoder(input).Decode(&request); err != nil {
		return err
	}
	answer, err := RelayWait(folder, request)
	if err != nil {
		return err
	}
	behavior := "deny"
	if answer == "allow" {
		behavior = "allow"
	}
	return json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest", "decision": map[string]string{"behavior": behavior},
	}})
}

func RelayWait(folder string, request RelayRequest) (string, error) {
	request.AskedAt = time.Now()
	if err := writeRelayJSON(folder, "relay.json", request); err != nil {
		return "", err
	}
	defer os.Remove(filepath.Join(folder, "relay.json"))
	timer := time.NewTimer(relayDeadline)
	defer timer.Stop()
	ticker := time.NewTicker(relayPollInterval)
	defer ticker.Stop()
	answer := ""
wait:
	for {
		answer = readAnswer(folder)
		if answer != "" {
			break
		}
		select {
		case <-timer.C:
			answer = "denied: no answer"
			break wait
		case <-ticker.C:
		}
	}
	// The polling loop must retain the decision after the answer marker is consumed.
	if err := writeRelayJSON(folder, "relay-answer.json", relayAnswer{request, answer, time.Now()}); err != nil {
		return "", err
	}
	return answer, nil
}

type relayTracker struct {
	request *RelayRequest
	total   time.Duration
}

func (r *relayTracker) open() bool { return r.request != nil }

func (r *relayTracker) elapsed(now time.Time) time.Duration {
	if r.open() {
		return r.total + now.Sub(r.request.AskedAt)
	}
	return r.total
}

func relayText(request RelayRequest) string {
	return request.ToolName + " " + string(request.ToolInput)
}

func (r *relayTracker) start(env *Env, t *Task, rec *Record, name string, request RelayRequest) {
	r.request = &request
	text := relayText(request)
	rec.Events = append(rec.Events, Event{At: request.AskedAt, Kind: "relay", Text: text})
	_ = rec.Save()
	ui.Eventf("relay", "task %s  %s asks:\n%s", t.Task, name, text)
	env.Solo.Wake("relay", t.Task, t.Title, name)
}

func (r *relayTracker) finish(rec *Record, answer string, now time.Time) {
	seconds := now.Sub(r.request.AskedAt).Seconds()
	rec.Events = append(rec.Events, Event{At: now, Kind: "relay-answer", Text: answer})
	b, _ := json.Marshal(map[string]any{"tool": r.request.ToolName, "input": cut(string(r.request.ToolInput), 300), "answer": answer, "seconds": seconds})
	rec.Permission.Relays = append(rec.Permission.Relays, b)
	r.total += now.Sub(r.request.AskedAt)
	r.request = nil
	_ = rec.Save()
}

func (r *relayTracker) poll(env *Env, t *Task, rec *Record, name string, now time.Time) {
	var request RelayRequest
	b, err := os.ReadFile(filepath.Join(t.Folder, "relay.json"))
	present := err == nil && json.Unmarshal(b, &request) == nil
	var answer relayAnswer
	b, err = os.ReadFile(filepath.Join(t.Folder, "relay-answer.json"))
	if err == nil && json.Unmarshal(b, &answer) == nil && (!present || request.AskedAt != answer.Request.AskedAt) {
		if !r.open() {
			r.start(env, t, rec, name, answer.Request)
		}
		r.finish(rec, answer.Answer, answer.AnsweredAt)
		_ = os.Remove(filepath.Join(t.Folder, "relay-answer.json"))
	}
	if present && !r.open() {
		r.start(env, t, rec, name, request)
	} else if !present && r.open() {
		r.finish(rec, "denied: no answer", now)
	}
}
