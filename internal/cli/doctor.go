package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/spf13/cobra"
)

type doctorRow struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state"`
}

func (a *app) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Harnesses, versions, logins, the Jev key, and Solo",
		Long:  "Refresh the discovery cache and report every Harness, the Jev key, Solo, the config files, and the Roster's pre-flight state.",
		RunE: func(_ *cobra.Command, _ []string) error {
			env, err := a.env(true)
			if err != nil {
				return err
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
					row.State = "missing"
					problems++
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
				rows = append(rows, row)
			}
			rows = append(rows, jevRow())
			rows = append(rows, soloRow())
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
			rt := &router.Router{Roster: env.Roster, Disc: env.Disc, Reg: env.Reg}
			failing := 0
			for _, f := range env.Roster.Families {
				if f.Enabled && rt.Preflight(f, "read") != "" && rt.Preflight(f, "edit") != "" {
					failing++
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
}

func jevRow() doctorRow {
	row := doctorRow{Name: "jev"}
	switch {
	case os.Getenv("TYPESAFE_API_KEY") != "":
		row.Detail = "key in TYPESAFE_API_KEY"
		row.State = "ok  (router lands in milestone 2)"
	case keyInEnvFile():
		row.Detail = "key in " + config.EnvFile()
		row.State = "ok  (router lands in milestone 2)"
	default:
		row.Detail = "no key"
		row.State = "rules table only (maestro key set, or TYPESAFE_API_KEY)"
	}
	return row
}

func keyInEnvFile() bool {
	b, err := os.ReadFile(config.EnvFile())
	return err == nil && strings.Contains(string(b), "TYPESAFE_API_KEY=")
}

func soloRow() doctorRow {
	if id := os.Getenv("SOLO_PROCESS_ID"); id != "" {
		return doctorRow{Name: "solo", Detail: "SOLO_PROCESS_ID " + id, State: "ok  (tab lanes land in milestone 6)"}
	}
	return doctorRow{Name: "solo", Detail: "not inside a Solo tab", State: "one-shot lanes only"}
}

var _ = ui.Quiet
