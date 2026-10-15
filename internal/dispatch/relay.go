package dispatch

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/ui"
)

var relayDeadline = 10 * time.Minute
var relayPollInterval = time.Second

type RelayRequest struct {
	ID        string          `json:"id"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id"`
	AskedAt   time.Time       `json:"asked_at"`
}

type relayLock struct {
	PID int       `json:"pid"`
	At  time.Time `json:"at"`
	ID  string    `json:"id"`
}

func readRelayLock(path string) (relayLock, bool) {
	var owner relayLock
	b, err := os.ReadFile(path)
	ok := err == nil && json.Unmarshal(b, &owner) == nil && owner.PID > 0
	return owner, ok
}

// Two waiting processes must not remove a new owner while they recover the
// same dead owner's lock. Hold the old file and check its identity first.
func removeDeadRelayLock(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	opened, err := f.Stat()
	if err != nil {
		return
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(opened, current) {
		return
	}
	var owner relayLock
	if json.NewDecoder(f).Decode(&owner) == nil && owner.PID > 0 && syscall.Kill(owner.PID, 0) == syscall.ESRCH {
		_ = os.Remove(path)
	}
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
	return writeRelayFile(folder, name, b)
}

// WriteRelayAnswer writes the complete id before it publishes the marker.
// RelayWait must not read a marker while its id is still being written.
func WriteRelayAnswer(folder, answer, relayID string) error {
	if answer != "allow" && answer != "deny" {
		return fmt.Errorf("the answer is allow or deny")
	}
	return writeRelayFile(folder, "answer-"+answer, []byte(relayID))
}

func writeRelayFile(folder, name string, b []byte) error {
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
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	request.ID = fmt.Sprintf("%x", id)
	timer := time.NewTimer(relayDeadline)
	defer timer.Stop()
	ticker := time.NewTicker(relayPollInterval)
	defer ticker.Stop()
	lockPath := filepath.Join(folder, "relay.lock")
	owner := relayLock{PID: os.Getpid(), At: time.Now().UTC(), ID: request.ID}
	lock, err := os.CreateTemp(folder, ".relay-lock-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(lock.Name())
	err = json.NewEncoder(lock).Encode(owner)
	closeErr := lock.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	for {
		// Link publishes the complete owner and fails if another lock exists.
		err := os.Link(lock.Name(), lockPath)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
		removeDeadRelayLock(lockPath)
		select {
		case <-timer.C:
			return "denied: no answer", nil
		case <-ticker.C:
		}
	}
	defer func() {
		if current, ok := readRelayLock(lockPath); ok && current == owner {
			_ = os.Remove(filepath.Join(folder, "relay.json"))
			_ = os.Remove(lockPath)
		}
	}()
	request.AskedAt = time.Now()
	if err := writeRelayJSON(folder, "relay.json", request); err != nil {
		return "", err
	}
	answer := ""
wait:
	for {
		if current, ok := readRelayLock(lockPath); !ok || current != owner {
			answer = "denied: no answer"
			break
		}
		answer = readAnswer(folder, request.ID)
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
	if err := writeRelayJSON(folder, "relay-answer-"+request.ID+".json", relayAnswer{request, answer, time.Now()}); err != nil {
		return "", err
	}
	return answer, nil
}

type relayTracker struct {
	request  *RelayRequest
	total    time.Duration
	answered map[string]bool
}

func (r *relayTracker) open() bool { return r.request != nil }

func (r *relayTracker) elapsed(now time.Time) time.Duration {
	if r.open() {
		return r.total + now.Sub(r.request.AskedAt)
	}
	return r.total
}

func relayText(request RelayRequest) string {
	return request.ID + " " + request.ToolName + " " + string(request.ToolInput)
}

func (r *relayTracker) start(env *Env, t *Task, rec *Record, name string, request RelayRequest) {
	r.request = &request
	text := relayText(request)
	rec.Events = append(rec.Events, Event{At: request.AskedAt, Kind: "relay", Text: text})
	_ = rec.Save()
	ui.Eventf("relay", "task %s  %s asks:\n%s", t.Task, name, text)
	env.Solo.Wake("relay", t.Task, t.Title, name, request.ID)
}

func (r *relayTracker) finish(rec *Record, answer string, now time.Time) {
	seconds := now.Sub(r.request.AskedAt).Seconds()
	rec.Events = append(rec.Events, Event{At: now, Kind: "relay-answer", Text: answer})
	b, _ := json.Marshal(map[string]any{"id": r.request.ID, "tool": r.request.ToolName, "input": cut(string(r.request.ToolInput), 300), "answer": answer, "seconds": seconds})
	rec.Permission.Relays = append(rec.Permission.Relays, b)
	r.total += now.Sub(r.request.AskedAt)
	r.request = nil
	_ = rec.Save()
}

func (r *relayTracker) poll(env *Env, t *Task, rec *Record, name string, now time.Time) {
	folder, err := config.ControlDirForFolder(t.Folder)
	if err != nil {
		return
	}
	if r.answered == nil {
		r.answered = map[string]bool{}
	}
	type completed struct {
		path   string
		answer relayAnswer
	}
	var answers []completed
	paths, _ := filepath.Glob(filepath.Join(folder, "relay-answer-*.json"))
	for _, path := range paths {
		var answer relayAnswer
		b, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(b, &answer) == nil {
			answers = append(answers, completed{path, answer})
		}
	}
	sort.Slice(answers, func(i, j int) bool {
		return answers[i].answer.Request.AskedAt.Before(answers[j].answer.Request.AskedAt)
	})
	for _, saved := range answers {
		answer := saved.answer
		if !r.answered[answer.Request.ID] {
			previous := r.request
			if previous == nil || previous.ID != answer.Request.ID {
				r.start(env, t, rec, name, answer.Request)
			}
			r.finish(rec, answer.Answer, answer.AnsweredAt)
			r.answered[answer.Request.ID] = true
			if previous != nil && previous.ID != answer.Request.ID {
				r.request = previous
			}
		}
		_ = os.Remove(saved.path)
	}
	var request RelayRequest
	b, err := os.ReadFile(filepath.Join(folder, "relay.json"))
	present := err == nil && json.Unmarshal(b, &request) == nil && !r.answered[request.ID]
	if r.open() && (!present || r.request.ID != request.ID) {
		// The owner can finish between the answer scan and the request read.
		if _, err := os.Stat(filepath.Join(folder, "relay-answer-"+r.request.ID+".json")); err == nil {
			return
		}
		r.finish(rec, "denied: no answer", now)
	}
	if present && !r.open() {
		r.start(env, t, rec, name, request)
	}
}
