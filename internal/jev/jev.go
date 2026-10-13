// Package jev is the client for TypeSafe's System One endpoint: one POST with
// a state and typed questions (Choice, Score, Noul), typed answers back.
// Jev never writes text; Maestro uses it only to classify what it is shown.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/redact"
)

// DefaultURL and DefaultModel are the embedded defaults; the roster's router
// block overrides them.
const (
	DefaultURL   = "https://api.typesafe.ai/v1/systemone"
	DefaultModel = "jev-1.13.0"
)

// ErrNoKey means no key was found in the environment or ~/.maestro/env.
var ErrNoKey = errors.New("no Jev key: set TYPESAFE_API_KEY or run maestro key set")

// Client talks to one System One endpoint.
type Client struct {
	URL     string
	Model   string
	Key     string
	Timeout time.Duration
	HTTP    *http.Client
	Redact  []string
}

// New builds a client from the router block and the key on this machine.
func New(url, model string, timeout time.Duration) (*Client, error) {
	key, err := Key()
	if err != nil {
		return nil, err
	}
	if url == "" {
		url = DefaultURL
	}
	if model == "" {
		model = DefaultModel
	}
	if timeout == 0 {
		timeout = 1500 * time.Millisecond
	}
	return &Client{URL: url, Model: model, Key: key, Timeout: timeout, HTTP: &http.Client{}}, nil
}

// Key reads TYPESAFE_API_KEY from the environment, else from ~/.maestro/env.
func Key() (string, error) {
	if k := os.Getenv("TYPESAFE_API_KEY"); k != "" {
		return k, nil
	}
	b, err := os.ReadFile(config.EnvFile())
	if err != nil {
		return "", ErrNoKey
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TYPESAFE_API_KEY=") {
			key := strings.Trim(strings.TrimPrefix(line, "TYPESAFE_API_KEY="), `"'`)
			if key != "" {
				return key, nil
			}
		}
	}
	return "", ErrNoKey
}

// SaveKey writes ~/.maestro/env with mode 0600.
func SaveKey(key string) error {
	if err := config.EnsureHome(); err != nil {
		return err
	}
	return os.WriteFile(config.EnvFile(), []byte("TYPESAFE_API_KEY="+key+"\n"), 0o600)
}

// Question is one typed question. Criteria is a map for Choice, an ordered
// list for Score, and absent for Noul.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Choice builds a Choice question over named options with descriptions.
func Choice(instructions string, options map[string]any) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: options}
}

// Score builds a Score question over an ordered scale.
func Score(instructions string, levels []string) Question {
	return Question{Type: "score", Instructions: instructions, Criteria: levels}
}

// Noul builds a yes/no question.
func Noul(instructions string) Question {
	return Question{Type: "noul", Instructions: instructions}
}

// Answer is one typed answer.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Response is the endpoint's reply.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Elapsed time.Duration `json:"-"`
}

// HTTPError carries the status code so callers can retry 429 and 529.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("jev: HTTP %d: %s", e.Status, cut(e.Body, 200))
}

// Ask sends one request. It retries once on 429 or 529 and gives up at the
// client's timeout, which the Router treats as "Jev absent".
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	body, err := redact.JSON(map[string]any{"model": c.Model, "state": state, "questions": questions}, c.Redact)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := c.once(ctx, body)
		if err == nil {
			resp.Elapsed = time.Since(start)
			return resp, nil
		}
		last = err
		var he *HTTPError
		if errors.As(err, &he) && (he.Status == 429 || he.Status == 529) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		break
	}
	return nil, last
}

func (c *Client) once(ctx context.Context, body []byte) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return nil, &HTTPError{Status: res.StatusCode, Body: string(data)}
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("jev: bad response: %w", err)
	}
	return &out, nil
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
