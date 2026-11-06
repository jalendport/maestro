package worktree

import (
	"fmt"
	"regexp"
	"strings"
)

// Eligible reports whether checks can run against the committed project files
// and its shared links in a Worktree. It does not change the repository.
func Eligible(root string, touch [][]string, checks []string, links []string) (ok bool, reason string) {
	if _, err := head(root, "HEAD"); err != nil {
		return false, "tree has no git history"
	}
	if _, err := git(root, "", "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return false, "HEAD is not on a branch"
	}
	for _, check := range checks {
		word := checkWord(check)
		switch word {
		case "ddev", "docker", "docker-compose", "lando", "sail":
		default:
			if !strings.HasSuffix(word, "/sail") {
				continue
			}
		}
		return false, "checks run in a container bound to the project folder (" + word + ")"
	}
	for i, patterns := range touch {
		found, err := link(root, "", links, patterns)
		if err != nil {
			return false, "cannot read dependencies: " + err.Error()
		}
		if len(found.Refused) > 0 {
			return false, fmt.Sprintf("task %d touches %s", i+1, found.Refused[0])
		}
	}
	return true, ""
}

// checkWord strips assignments and env, including env -i and env -u NAME,
// as the hook does. Quoted environment values stay together.
func checkWord(command string) string {
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
		} else if ch == ' ' || ch == '\t' || ch == '\n' {
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
	for len(words) > 0 {
		if environmentAssignment.MatchString(words[0]) {
			words = words[1:]
			continue
		}
		if words[0] != "env" {
			return words[0]
		}
		words = words[1:]
		for len(words) > 0 {
			if words[0] == "-i" {
				words = words[1:]
			} else if words[0] == "-u" && len(words) > 1 {
				words = words[2:]
			} else {
				break
			}
		}
	}
	return ""
}

var environmentAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
