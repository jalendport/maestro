package harness

import "strings"

// classifyText maps vendor error text to a Signal class. Unknown text is a
// Transient error; the caller records it.
func classifyText(harness, text string) Signal { return ClassifyText(harness, text) }

// ClassifyText is classifyText for other packages.
func ClassifyText(harness, text string) Signal {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "sandbox profile"), strings.Contains(t, "could not apply"):
		return Signal{Class: "lane", Text: text}
	case strings.Contains(t, "usage limit"), strings.Contains(t, "rate limit reached"), strings.Contains(t, "you've hit your limit"), strings.Contains(t, "reached your"), strings.Contains(t, "limit reached"), strings.Contains(t, "quota exceeded"):
		return Signal{Class: "limit", Text: text}
	case strings.Contains(t, "429"):
		if harness == "agy" {
			return Signal{Class: "transient", Text: text}
		}
		return Signal{Class: "limit", Text: text}
	case strings.Contains(t, "billing"), strings.Contains(t, "not logged in"), strings.Contains(t, "login"), strings.Contains(t, "unauthorized"), strings.Contains(t, "unknown model"), strings.Contains(t, "model not found"), strings.Contains(t, "invalid model"), strings.Contains(t, "issue with the selected model"):
		return Signal{Class: "lane", Text: text}
	}
	return Signal{Class: "transient", Text: text}
}
