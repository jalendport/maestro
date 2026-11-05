package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/spf13/cobra"
)

type doctorRow struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state"`
}

func (a *app) doctorCmd() *cobra.Command {
	var canary bool
	c := &cobra.Command{
		Use:   "doctor [--canary [harness]]",
		Short: "Check Harnesses, versions, logins, the Jev key, and Solo",
		Long:  "Read current discovery facts and report every Harness, its Canary result, the Jev key, Solo, the config files, and the Roster's pre-flight state. --canary starts one small read Dispatch per installed Harness, or for the named Harness, and uses your Account.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 && !canary {
				return fmt.Errorf("a Harness name needs --canary")
			}
			env, err := a.loadEnv(true, false)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				h, ok := env.Reg.Get(args[0])
				if !ok {
					return fmt.Errorf("unknown Harness %q", args[0])
				}
				if h.Bin() == "" {
					return fmt.Errorf("Harness %s is not installed", args[0])
				}
			}
			var rows []doctorRow
			problems := 0
			for _, name := range env.Reg.Names() {
				rec := env.Disc.Harness[name]
				h, _ := env.Reg.Get(name)
				row := doctorRow{Name: name}
				switch {
				case rec.Path == "":
					row.Detail = "not installed"
					row.State = "not installed"
				default:
					row.Detail = fmt.Sprintf("%-8s %s", rec.Version, rec.Path)
					if rec.LoginHint != "" {
						row.Detail += "  login: " + rec.LoginHint
					}
					min := env.Roster.Harness[name].MinVersion
					switch {
					case rec.Error != "":
						row.State = rec.Error
						problems++
					case min != "" && rec.Version != "" && harness.VersionLess(rec.Version, min):
						row.State = fmt.Sprintf("upgrade to %s", min)
						problems++
					case h.Enforcement("edit") == harness.NoShape && h.Enforcement("read") == harness.NoShape:
						row.State = "catalog only in this build"
					case h.Enforcement("read") == harness.BestEffort:
						row.State = "ok  (read = best-effort: no automatic read work)"
					default:
						row.State = "ok"
					}
				}
				if rec.Path != "" {
					if canary && (len(args) == 0 || args[0] == name) {
						if _, err := dispatch.RunCanary(env, name); err != nil {
							return err
						}
					}
					result, err := dispatch.LoadCanary(name)
					if err != nil {
						return err
					}
					state, failed := canaryState(result, rec.Version, name)
					row.State += "  " + state
					if failed {
						problems++
					}
				}
				rows = append(rows, row)
			}
			rows = append(rows, jevRow())
			rows = append(rows, orchestratorRow(env.Roster.Router.Orchestrator))
			rows = append(rows, soloRow(env))
			cfg := doctorRow{Name: "config", Detail: config.UserFile()}
			if _, err := os.Stat(config.UserFile()); err != nil {
				cfg.Detail += " (absent: embedded defaults only)"
			}
			if _, err := os.Stat(config.ProjectFile(env.Root)); err == nil {
				cfg.Detail += "  project " + config.ProjectFile(env.Root)
			} else {
				cfg.Detail += "  project file: none"
			}
			cfg.State = "ok"
			rows = append(rows, cfg)
			user, _, err := config.LoadUser()
			if err != nil {
				return err
			}
			for _, key := range user.Ignored {
				rows = append(rows, doctorRow{Name: "config", Detail: config.UserFile(), State: key + " has no effect"})
			}
			// User warnings were printed once by loadEnv. Project keys are kept
			// by LoadProject so doctor can report them without decoding again.
			for _, key := range env.Project.Unknown {
				rows = append(rows, doctorRow{Name: "config", Detail: config.ProjectFile(env.Root), State: fmt.Sprintf("unknown key %q (ignored)", key)})
			}
			rt := env.NewRouter()
			failing := 0
			for _, f := range env.Roster.Families {
				if f.Enabled && rt.Preflight(f, "read") != "" && rt.Preflight(f, "edit") != "" {
					failing++
				}
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			for _, name := range adapter.Harnesses {
				state := adapter.State(name, home)
				detail := fmt.Sprintf("skill %s, link %s, hook %s, reminder %s", state.Skill, state.Link, state.Hook, state.Reminder)
				if state.HookReason != "" {
					detail += "; " + state.HookReason
				}
				rows = append(rows, doctorRow{Name: "adapter " + name, Detail: detail, State: state.Overall})
				if env.Disc.Harness[name].Path != "" && (state.Overall == "partial" || state.Overall == "wrong") {
					problems++
				}
			}
			rows = append(rows, doctorRow{Name: "roster", Detail: fmt.Sprintf("%d families", len(env.Roster.Families)), State: fmt.Sprintf("%d fail pre-flight (see maestro roster)", failing)})
			if a.json {
				b, _ := json.MarshalIndent(rows, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
			} else {
				w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				for _, r := range rows {
					fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Detail, r.State)
				}
				w.Flush()
			}
			if problems > 0 {
				return &proc.ExitError{Code: proc.CodeFailed}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&canary, "canary", false, "Run one Canary per installed Harness, or for the named Harness (uses your Account)")
	return c
}

func canaryState(result *dispatch.CanaryResult, version, name string) (string, bool) {
	if result == nil || version == "" || result.Version != version || result.Harness != name {
		return "no canary result for installed version; run maestro doctor --canary " + name, false
	}
	if result.Passed() {
		return "canary passed for " + version, false
	}
	return "canary failed for " + version + " (" + result.FailedParts() + ")", true
}

func orchestratorRow(family string) doctorRow {
	if family == "" {
		family = "not set"
	}
	return doctorRow{Name: "orchestrator", Detail: family, State: "info"}
}

func jevRow() doctorRow {
	row := doctorRow{Name: "jev"}
	key, err := jev.Key()
	if err != nil || key == "" {
		row.Detail = "no key"
		row.State = "rules table only; edits will end unverified"
	} else {
		if os.Getenv("TYPESAFE_API_KEY") != "" {
			row.Detail = "key in TYPESAFE_API_KEY"
		} else {
			row.Detail = "key in " + config.EnvFile()
		}
		row.State = "ok"
	}
	return row
}

func soloRow(env *dispatch.Env) doctorRow {
	if env.Solo == nil {
		if os.Getenv("SOLO_PROCESS_ID") != "" {
			return doctorRow{Name: "solo", Detail: "SOLO_PROCESS_ID set, helper unreachable", State: "View unavailable"}
		}
		return doctorRow{Name: "solo", Detail: "not inside a Solo tab", State: "no Solo View"}
	}
	return doctorRow{Name: "solo", Detail: fmt.Sprintf("project %d, orchestrator process %d", env.Solo.ProjectID, env.Solo.Orchestrator), State: "ok"}
}
