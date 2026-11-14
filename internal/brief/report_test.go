package brief

import (
	"reflect"
	"strings"
	"testing"
)

func TestReportsMatchHeadingsInsideFences(t *testing.T) {
	for _, fence := range []string{"```md\nExample.", "```md\n```\nExample.\n```"} {
		t.Run(fence, func(t *testing.T) {
			r := ParseReport("## Summary\n" + fence + "\n## Blocked\n- Harness denied docker info.\n## Open\nnone\nSTATUS: partial\n")
			if r.Status != StatusPartial || !reflect.DeepEqual(r.Blocked, []string{"Harness denied docker info."}) || strings.Join(r.Order, ",") != "Summary,Blocked,Open" {
				t.Fatalf("Report lost headings: %+v", r)
			}
			c := ParseReport("## Answer\n" + fence + "\n## Findings\n- Finding.\n## Concerns\nnone\nPOSITION: confirmed\n")
			if c.Position != "confirmed" || strings.Join(c.Order, ",") != "Answer,Findings,Concerns" || c.Sections["Findings"] != "- Finding." {
				t.Fatalf("Council result lost headings: %+v", c)
			}
		})
	}
}

func TestParseReportDropsNoCommandPlaceholders(t *testing.T) {
	for _, subject := range []string{"command was", "commands were"} {
		for _, action := range []string{"denied", "blocked"} {
			for _, period := range []string{"", "."} {
				for _, upper := range []bool{false, true} {
					item := "No " + subject + " " + action + period
					if upper {
						item = strings.ToUpper(item)
					}
					t.Run(item, func(t *testing.T) {
						report := ParseReport("## Blocked\n- " + item + "\n- Harness denied `docker info`.\n\nSTATUS: partial\n")
						want := []string{"Harness denied `docker info`."}
						if !reflect.DeepEqual(report.Blocked, want) {
							t.Fatalf("Blocked = %q, want %q", report.Blocked, want)
						}
					})
				}
			}
		}
	}
}

func TestCleanListKeepsRealItems(t *testing.T) {
	want := []string{
		"Harness denied `docker info`.",
		"Done item 2 is unmet: the database is unavailable.",
		"No command was denied, but the required version is unavailable.",
		"No commands were blocked. Done item 3 is still unmet.",
	}
	items := append([]string{"none", "(none)", "n/a", "  NO COMMAND WAS DENIED.  "}, want...)
	if got := cleanList(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanList = %q, want %q", got, want)
	}
}
