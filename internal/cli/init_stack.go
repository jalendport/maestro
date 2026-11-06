package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type projectDefaults struct {
	dirs, allow, checks []string
	docker              bool
	links               []string
}

func detectProjectStack(root string) projectDefaults {
	var d projectDefaults
	has := func(names ...string) bool {
		for _, name := range names {
			if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
				return true
			}
		}
		return false
	}
	add := func(dirs, allow, checks []string) {
		for _, dir := range dirs {
			if !slices.Contains(d.dirs, dir) {
				d.dirs = append(d.dirs, dir)
			}
		}
		d.allow = append(d.allow, allow...)
		d.checks = append(d.checks, checks...)
	}
	if has("go.mod") {
		cache := "~/.cache/go-build"
		if runtime.GOOS == "darwin" {
			cache = "~/Library/Caches/go-build"
		}
		dirs := []string{cache, "~/go/pkg/mod"}
		if paths := toolchainCache(root, "go", "env", "GOCACHE", "GOMODCACHE"); len(paths) == 2 {
			dirs = paths
		}
		add(dirs, []string{"Bash(go test *)", "Bash(go vet *)", "Bash(go build *)", "Bash(gofmt *)"}, []string{"go test ./..."})
	}
	if has("package.json") {
		runner := "npm"
		dirs := []string{"~/.npm"}
		switch {
		case has("package-lock.json"):
		case has("pnpm-lock.yaml"):
			runner, dirs = "pnpm", []string{"~/.cache/pnpm"}
		case has("yarn.lock"):
			runner, dirs = "yarn", []string{"~/.yarn", "~/.cache/yarn"}
		}
		if runner == "npm" {
			if paths := toolchainCache(root, "npm", "config", "get", "cache"); len(paths) == 1 {
				dirs = paths
			}
		} else if runner == "pnpm" {
			if paths := toolchainCache(root, "pnpm", "store", "path"); len(paths) == 1 {
				dirs = paths
			}
		} else if runner == "yarn" {
			if paths := toolchainCache(root, "yarn", "cache", "dir"); len(paths) == 1 {
				dirs = []string{"~/.yarn", paths[0]}
			}
		}
		add(dirs, []string{"Bash(" + runner + " test)", "Bash(" + runner + " test *)", "Bash(" + runner + " run *)"}, []string{runner + " test"})
	}
	if has("composer.json") {
		var composer struct {
			Require map[string]json.RawMessage `json:"require"`
		}
		data, _ := os.ReadFile(filepath.Join(root, "composer.json"))
		_ = json.Unmarshal(data, &composer)
		if _, ok := composer.Require["craftcms/cms"]; ok {
			d.links = append(d.links, "storage", "web/cpresources", "config/license.key")
		}
		if _, ok := composer.Require["laravel/framework"]; ok {
			d.links = append(d.links, "storage", "bootstrap/cache")
		}
		dirs := []string{"~/.composer/cache", "~/.cache/composer"}
		if paths := toolchainCache(root, "composer", "config", "--global", "cache-dir"); len(paths) == 1 {
			dirs = paths
		}
		add(dirs, []string{"Bash(composer test)", "Bash(composer test *)", "Bash(vendor/bin/phpunit *)", "Bash(vendor/bin/pint *)", "Bash(vendor/bin/phpstan *)"}, []string{"composer test"})
	}
	if has("pyproject.toml", "requirements.txt") {
		dirs := []string{"~/.cache/pip"}
		if paths := toolchainCache(root, "pip", "cache", "dir"); len(paths) == 1 {
			dirs = paths
		}
		add(dirs, []string{"Bash(pytest)", "Bash(pytest *)", "Bash(ruff *)"}, []string{"pytest"})
	}
	if has("Cargo.toml") {
		add([]string{"~/.cargo"}, []string{"Bash(cargo test *)", "Bash(cargo build *)", "Bash(cargo clippy *)", "Bash(cargo fmt *)"}, []string{"cargo test"})
	}
	if has("Dockerfile", "compose.yaml", "compose.yml", "docker-compose.yml") {
		d.docker = true
		dirs := []string{"~/.docker"}
		if home, err := os.UserHomeDir(); err == nil {
			if info, err := os.Stat(filepath.Join(home, ".orbstack", "run")); err == nil && info.IsDir() {
				dirs = append(dirs, "~/.orbstack/run")
			}
		}
		add(dirs, []string{"Bash(docker compose *)", "Bash(docker build *)"}, []string{"docker compose config --quiet"})
	}
	return d
}

// proposeWorktreeLinks keeps an existing choice, including a commented key.
func proposeWorktreeLinks(root, source string, links []string) string {
	if len(links) == 0 {
		return source
	}
	tokens, comments := projectTokens(source)
	for _, comment := range comments {
		parts, _ := projectTokens(comment)
		if len(parts) > 1 && projectKey(parts[0].text) == "worktree_links" && parts[1].text == "=" {
			return source
		}
	}
	end := len(source)
	depth := 0
	for i, token := range tokens {
		if depth == 0 && token.text == "[" && (i == 0 || tokens[i-1].text != "=") {
			end = strings.LastIndex(source[:token.start], "\n") + 1
			break
		}
		if depth == 0 && projectKey(token.text) == "worktree_links" && i+1 < len(tokens) && tokens[i+1].text == "=" {
			return source
		}
		switch token.text {
		case "[", "{":
			depth++
		case "]", "}":
			depth--
		}
	}
	prefix := ""
	for _, name := range links {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			prefix = "# "
			break
		}
	}
	line := prefix + "worktree_links = " + projectArray(links) + "\n"
	if end > 0 && source[end-1] != '\n' {
		line = "\n" + line
	}
	return source[:end] + line + source[end:]
}

func toolchainCache(root, name string, args ...string) []string {
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if !filepath.IsAbs(line) && !strings.HasPrefix(line, "~/") {
			return nil
		}
		paths = append(paths, line)
	}
	return paths
}
