package hook

import (
	"strings"
	"testing"
)

func TestDecideLaneBaselineCommands(t *testing.T) {
	commands := []string{
		"sed -n '1,120p' main.go", "sed -s -e 's/old/new/g' main.go", "sed -ne '1,$p' main.go",
		"sed --expression='s#old#new#gp' main.go", "sed --quiet -e '/name/p' main.go",
		"sed -n '1,3{p;d;}' main.go", "sed 's/write/execute/g' main.go", "sed -e p -e q main.go",
		"sed -n '/a\\/b/p' main.go", "sed -n -- '1p' main.go",
		"wc -l main.go", "diff a b", "stat main.go", "file main.go", "which go", "tree .",
		"du -sh .", "df -h .", "basename main.go", "dirname main.go", "realpath main.go", "pwd",
		"cut -f 1 main.go", "sort main.go", "tr '[:lower:]' '[:upper:]'", "jq '.name' package.json",
		"yq '.name' config.yaml", "yq eval '.name' config.yaml", "test -f main.go", "[ -f main.go ]", "[\t-f main.go ]",
		"git ls-files", "git ls-files --stage", "git describe --tags", "git rev-parse --show-toplevel",
		"git tag", "git tag -l 'v*'", "git tag --list 'v*'", "git tag -n", "git tag -n3",
		"git tag --contains", "git tag --contains HEAD", "git tag --points-at HEAD", "git tag --sort version:refname",
		"git tag --contains=HEAD", "git tag --points-at=HEAD", "git tag --sort=version:refname", "git tag --list -- 'v*'",
		"go version", "go version -m ./maestro",
		"go env", "go env -json", "go env GOPATH", "go env --json",
		"go list ./...", "go list -json ./...", "go list -m all", "go list -mod=readonly ./...", "go list -mod vendor ./...",
		"go list -export ./...", "go list -compiled ./...", "go list -modcacherw ./...",
		"go doc fmt.Println", "go doc -short fmt", "go doc -all fmt",
		"node --version", "npm --version", "pnpm --version", "yarn --version", "php --version",
		"npm ls", "npm ls --all", "npm view react version", "pnpm ls", "pnpm ls --depth 0",
		"composer --version", "composer show", "composer show --format=json", "python3 --version",
		"pip --version", "pip list", "pip list --format=json", "pip show requests", "cargo --version", "rustc --version",
		"docker --version", "docker version", "docker info", "docker ps -a", "docker images",
		"docker compose ps", "docker compose ps --all", "docker compose config", "docker compose config --services",
	}
	for _, access := range []string{"read", "edit"} {
		for _, command := range commands {
			t.Run(access+"/"+command, func(t *testing.T) {
				call := &Call{Harness: "agy", Tool: "run_command", Input: map[string]any{"CommandLine": command}}
				if got := DecideLane(call, &Lane{Access: access, Root: "/project"}); got.Kind != "auto_approve" {
					t.Fatalf("verdict = %+v", got)
				}
			})
		}
	}
}

func TestDecideLaneBaselineWriteForms(t *testing.T) {
	commands := []string{
		"sed -i 's/a/b/' file", "sed -i.bak 's/a/b/' file", "sed -I.bak 's/a/b/' file",
		"sed --in-place 's/a/b/' file", "sed --in-place=.bak 's/a/b/' file", "sed -ni 'p' file",
		"sed 'w output' file", "sed 'W output' file", "sed 'e touch output' file",
		"sed -s 'w output' file", "sed -s -e '1w output' file", "sed -n '1,3w output' file",
		"sed 's/a/b/e' file", "sed 's/a/b/ge' file", "sed 's/a/b/w output' file",
		"sed 's#a#b#e' file", "sed 's/a/b/ e' file", "sed -e p -e 'w output' file",
		"sed -ne'w output' file", "sed -n '1{p;w output;}' file", "sed -n '/a/e touch output' file",
		"sed -f script.sed file", "sed --file=script.sed file", "sed --expression='w output' file",
		"sed --unknown 'p' file", "sed -e", "sed -n 's/a/b' file", "sed -n '{p' file",
		"sed -n 'r file' file", "sed -n 's/[a]/b/' file", "sed -n 's/a/\\c/' file",
		"tree -o output", "tree -ooutput", "sort -o output file", "sort --output=output file",
		"file -C", "file --compile", "file -z archive.gz", "file --uncompress archive.gz",
		"file -Z archive.gz", "file --uncompress-noreport archive.gz",
		"file -p main.go", "file -bp main.go", "file --preserve-date main.go",
		"yq -i '.a = 1' file", "yq --inplace '.a = 1' file", "yq --in-place '.a = 1' file",
		"yq -s '.name' file", "yq --split-exp '.name' file", "yq --split-exp-file expression file",
		"git tag v1", "git tag -a v1", "git tag -d v1", "git tag -f v1", "git tag -s v1",
		"git tag -m message v1", "git tag -F message.txt v1", "git tag --local-user key v1",
		"git tag --create-reflog v1", "git tag --list -a v1", "git tag -- v1", "git tag -v v1",
		"git tag --points-at", "git tag --sort", "git tag --unknown",
		"git describe --dirty", "git describe --dirty=modified", "git describe --broken", "git describe --broken=broken",
		"pip show --log=output requests", "pip list --log=output",
		"go env -w GOPROXY=off", "go env -u GOPROXY", "go env -w=GOPROXY=off",
		"go env --w GOPROXY=off", "go env --u GOPROXY",
		"go list -mod=mod ./...", "go list -mod mod ./...", "go list --mod=mod ./...", "go list --mod mod ./...",
		"go list -modfile=other.mod ./...", "go list -modfile other.mod ./...", "go list -o output ./...",
		"go list -toolexec=program ./...", "go list -debug-actiongraph=output ./...", "go list -pkgdir=output ./...",
		"go list -asmflags=-debug=output ./...", "go list -gcflags=-logjson=output ./...",
		"go list -gccgoflags=-ooutput ./...", "go list -ldflags=-ooutput ./...",
		"go doc -http fmt", "go doc --http fmt",
		"docker compose config -o output", "docker compose config -ooutput", "docker compose config --output=output",
		"docker compose config --lock-image-digests",
	}
	for _, access := range []string{"read", "edit"} {
		for _, command := range commands {
			t.Run(access+"/"+command, func(t *testing.T) {
				lane := &Lane{Access: access, Root: "/project", Checks: []string{command}, Allow: []string{"Bash(" + command + ")"}}
				got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, lane)
				if got.Kind != "deny" || got.Reason == "" {
					t.Fatalf("verdict = %+v", got)
				}
			})
		}
	}
	for name := range readOnlyCommands {
		if !strings.HasSuffix(name, " --version") {
			continue
		}
		command := name + " --eval 'write()'"
		got := DecideLane(&Call{Tool: "run_command", Input: map[string]any{"CommandLine": command}}, &Lane{Access: "edit", Root: "/project", Checks: []string{name}})
		if got.Kind != "deny" {
			t.Fatalf("%s = %+v", command, got)
		}
	}
}

func TestDecideLaneBaselineExcludesChecks(t *testing.T) {
	commands := []string{
		"go test ./...", "npm test", "composer test", "pytest", "cargo test", "cargo metadata",
		"go build ./...", "go fmt ./...", "npm install", "pnpm install", "yarn install", "pip install requests",
		"node script.js", "python3 script.py", "php script.php", "rustc main.rs", "docker compose up",
	}
	for _, command := range commands {
		for _, access := range []string{"read", "edit"} {
			lane := &Lane{Access: access, Root: "/project"}
			call := &Call{Harness: "grok", Tool: "run_terminal_command", Input: map[string]any{"command": command}}
			if got := DecideLane(call, lane); got.Kind != "deny" {
				t.Fatalf("%s %s = %+v", access, command, got)
			}
			lane.Checks = []string{command}
			want := "deny"
			if access == "edit" {
				want = "auto_approve"
			}
			if got := DecideLane(call, lane); got.Kind != want {
				t.Fatalf("explicit check %s %s = %+v", access, command, got)
			}
		}
	}
}

func TestDecideLaneBaselineDenyReasonNamesCommands(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		got := DecideLane(&Call{Harness: "grok", Tool: "run_terminal_command", Input: map[string]any{"command": "unknown"}}, &Lane{Access: access, Root: "/project"})
		if got.Kind != "deny" {
			t.Fatal(got)
		}
		for command := range readOnlyCommands {
			if !strings.Contains(got.Reason, command) {
				t.Fatalf("deny reason omits %s: %s", command, got.Reason)
			}
		}
	}
}
