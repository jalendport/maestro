package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jalendport/maestro/internal/config"
)

func projectArray(values []string) string {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(map[string][]string{"values": values}); err != nil {
		panic(err)
	}
	return strings.TrimSpace(strings.TrimPrefix(b.String(), "values = "))
}

type projectToken struct {
	start, end int
	text       string
}

// Keep source offsets so a merge never rewrites the user's TOML or comments.
func projectTokens(source string) (tokens []projectToken, comments []string) {
	for i := 0; i < len(source); {
		start := i
		c := source[i]
		switch {
		case strings.ContainsRune(" \t\r\n", rune(c)):
			i++
			continue
		case c == '#':
			for i < len(source) && source[i] != '\n' {
				i++
			}
			comments = append(comments, source[start+1:i])
			continue
		case c == '"' || c == '\'':
			delimiter := string(c)
			if strings.HasPrefix(source[i:], strings.Repeat(delimiter, 3)) {
				delimiter = strings.Repeat(delimiter, 3)
			}
			i += len(delimiter)
			for i < len(source) {
				if c == '"' && source[i] == '\\' {
					i += 2
					continue
				}
				if strings.HasPrefix(source[i:], delimiter) {
					i += len(delimiter)
					// A multiline string can end with one or two extra quotes.
					if len(delimiter) == 3 {
						for n := 0; n < 2 && i < len(source) && source[i] == c; n++ {
							i++
						}
					}
					break
				}
				i++
			}
		case strings.ContainsRune("=[]{}.,", rune(c)):
			i++
		default:
			for i < len(source) && !strings.ContainsRune(" \t\r\n#\"'=[]{}.,", rune(source[i])) {
				i++
			}
		}
		if i > len(source) {
			i = len(source)
		}
		tokens = append(tokens, projectToken{start, i, source[start:i]})
	}
	return tokens, comments
}

func projectString(token string) (string, bool) {
	if !strings.HasPrefix(token, "\"") && !strings.HasPrefix(token, "'") {
		return "", false
	}
	var value struct{ Value string }
	_, err := toml.Decode("Value = "+token, &value)
	return value.Value, err == nil
}

func projectKey(token string) string {
	if value, ok := projectString(token); ok {
		return value
	}
	return token
}

func mergeProjectDefaults(source string, defaults projectDefaults) (string, error) {
	var project config.Project
	metadata, err := toml.Decode(source, &project)
	if err != nil {
		return "", err
	}
	tokens, comments := projectTokens(source)
	var commentedValues []string
	checksPresent := metadata.IsDefined("checks")
	for _, comment := range comments {
		parts, _ := projectTokens(comment)
		if len(parts) > 1 && projectKey(parts[0].text) == "checks" && parts[1].text == "=" {
			checksPresent = true
		}
		for _, part := range parts {
			if value, ok := projectString(part.text); ok {
				commentedValues = append(commentedValues, value)
			}
		}
	}
	type insertion struct {
		at   int
		text string
	}
	var insertions []insertion
	rootEnd := len(source)
	arrays := map[string][2]int{}
	depth := 0
	for i, token := range tokens {
		if depth == 0 && token.text == "[" && (i == 0 || tokens[i-1].text != "=") {
			rootEnd = strings.LastIndex(source[:token.start], "\n") + 1
			break
		}
		key := projectKey(token.text)
		if depth == 0 && (i == 0 || tokens[i-1].text != ".") && (key == "dirs" || key == "allow") && i+2 < len(tokens) && tokens[i+1].text == "=" && tokens[i+2].text == "[" {
			for j := i + 3; j < len(tokens); j++ {
				if tokens[j].text == "]" {
					arrays[key] = [2]int{i + 2, j}
					break
				}
			}
		}
		switch token.text {
		case "[", "{":
			depth++
		case "]", "}":
			depth--
		}
	}
	var additions strings.Builder
	for _, field := range []struct {
		key              string
		current, desired []string
	}{
		{"dirs", project.Dirs, defaults.dirs},
		{"allow", project.Allow, defaults.allow},
	} {
		var missing []string
		for _, value := range field.desired {
			if !slices.Contains(field.current, value) && !slices.Contains(commentedValues, value) && !slices.Contains(missing, value) {
				missing = append(missing, value)
			}
		}
		if len(missing) == 0 {
			continue
		}
		span, ok := arrays[field.key]
		if !ok {
			if metadata.IsDefined(field.key) {
				return "", fmt.Errorf("cannot find the %s array", field.key)
			}
			fmt.Fprintf(&additions, "%s = %s\n", field.key, projectArray(missing))
			continue
		}
		open, close := span[0], span[1]
		values := strings.TrimSuffix(strings.TrimPrefix(projectArray(missing), "["), "]")
		insertions = append(insertions, insertion{tokens[close].start, "\n  " + values + ",\n"})
		if close > open+1 && tokens[close-1].text != "," {
			insertions = append(insertions, insertion{tokens[close-1].end, ","})
		}
	}
	if !checksPresent && len(defaults.checks) > 0 {
		fmt.Fprintf(&additions, "# checks = %s\n", projectArray(defaults.checks))
	}
	if defaults.docker && !slices.Contains(project.Allow, "Bash(docker run *)") && !slices.Contains(commentedValues, "Bash(docker run *)") {
		additions.WriteString("# allow = [\"Bash(docker run *)\"]   # Review host mounts before you allow this command.\n")
	}
	if additions.Len() > 0 {
		prefix := ""
		if rootEnd > 0 && source[rootEnd-1] != '\n' {
			prefix = "\n"
		}
		insertions = append(insertions, insertion{rootEnd, prefix + additions.String()})
	}
	slices.SortStableFunc(insertions, func(a, b insertion) int { return b.at - a.at })
	for _, insertion := range insertions {
		source = source[:insertion.at] + insertion.text + source[insertion.at:]
	}
	var merged config.Project
	if _, err := toml.Decode(source, &merged); err != nil {
		return "", err
	}
	return source, nil
}
