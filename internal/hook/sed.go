package hook

import "strings"

// Script files and unknown syntax cannot be checked from the tool call.
func unsafeSedForm(words []string) string {
	var scripts []string
	options := true
	for i := 0; i < len(words); i++ {
		word := words[i]
		if options && word == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(word, "--") {
			switch {
			case word == "--quiet", word == "--silent", word == "--regexp-extended", word == "--separate", word == "--unbuffered", word == "--posix", word == "--sandbox", word == "--null-data":
				continue
			case word == "--expression":
				if i+1 >= len(words) {
					return "sed --expression without a script"
				}
				i++
				scripts = append(scripts, words[i])
			case strings.HasPrefix(word, "--expression="):
				scripts = append(scripts, strings.TrimPrefix(word, "--expression="))
			default:
				return "sed option: " + word
			}
			continue
		}
		if options && strings.HasPrefix(word, "-") && word != "-" {
			for j := 1; j < len(word); j++ {
				switch word[j] {
				case 'n', 'E', 'r', 's', 'u':
				case 'e':
					if j+1 < len(word) {
						scripts = append(scripts, word[j+1:])
					} else {
						if i+1 >= len(words) {
							return "sed -e without a script"
						}
						i++
						scripts = append(scripts, words[i])
					}
					j = len(word)
				default:
					return "sed option: " + word
				}
			}
			continue
		}
		if len(scripts) == 0 {
			scripts = append(scripts, word)
		}
	}
	if len(scripts) == 0 {
		return "sed without a script"
	}
	for _, script := range scripts {
		if !readOnlySedScript(script) {
			return "sed script cannot be read-only: " + script
		}
	}
	return ""
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range s {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func readOnlySedScript(script string) bool {
	depth := 0
	for i := 0; i < len(script); {
		for i < len(script) && strings.ContainsRune(" \t;", rune(script[i])) {
			i++
		}
		if i == len(script) {
			break
		}
		if script[i] == '}' {
			depth--
			if depth < 0 {
				return false
			}
			i++
			continue
		}
		for address := 0; address < 2; address++ {
			if i < len(script) && script[i] == '/' {
				var ok bool
				i, ok = sedDelimited(script, i+1, '/', true)
				if !ok {
					return false
				}
			} else {
				for i < len(script) && (script[i] >= '0' && script[i] <= '9' || script[i] == '$') {
					i++
				}
			}
			if address != 0 || i == len(script) || script[i] != ',' {
				break
			}
			i++
		}
		for i < len(script) && strings.ContainsRune(" \t!", rune(script[i])) {
			i++
		}
		if i == len(script) {
			return false
		}
		command := script[i]
		i++
		switch command {
		case '{':
			depth++
			continue
		case 'p', 'P', 'd', 'D', 'n', 'N', 'g', 'G', 'h', 'H', 'x', '=', 'z', 'F':
		case 'q', 'Q', 'l':
			for i < len(script) && (script[i] == ' ' || script[i] >= '0' && script[i] <= '9') {
				i++
			}
		case 's':
			if i == len(script) {
				return false
			}
			delim := script[i]
			if delim <= ' ' || delim >= 127 || strings.ContainsRune("\\[]", rune(delim)) || delim >= 'a' && delim <= 'z' || delim >= 'A' && delim <= 'Z' || delim >= '0' && delim <= '9' {
				return false
			}
			var ok bool
			i, ok = sedDelimited(script, i+1, delim, true)
			if !ok {
				return false
			}
			i, ok = sedDelimited(script, i, delim, false)
			if !ok {
				return false
			}
			for i < len(script) && strings.ContainsRune("gpIiMm0123456789", rune(script[i])) {
				i++
			}
		default:
			return false
		}
		for i < len(script) && (script[i] == ' ' || script[i] == '\t') {
			i++
		}
		if i < len(script) && script[i] != ';' && script[i] != '}' {
			return false
		}
	}
	return depth == 0
}

func sedDelimited(script string, i int, delim byte, pattern bool) (int, bool) {
	for i < len(script) {
		ch := script[i]
		i++
		if ch == delim {
			return i, true
		}
		// Bracket expressions and control escapes have dialect-specific rules.
		if ch < ' ' || ch >= 127 || pattern && ch == '[' {
			return i, false
		}
		if ch == '\\' {
			if i == len(script) || script[i] == 'c' || script[i] < ' ' || script[i] >= 127 {
				return i, false
			}
			i++
		}
	}
	return i, false
}
