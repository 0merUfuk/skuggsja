package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/audit"
)

func TestSummarySeparatesReadOnlyAccessFromSourceActivity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		observation string
		audit       audit.Comparison
		skipped     bool
		want        []string
		absent      []string
	}{
		{
			name: "quiet", observation: "observed", audit: audit.Comparison{Files: 8, Verified: true},
			want: []string{"no concurrent changes observed across 8 files"},
		},
		{
			name: "incomplete equality is neutral", observation: "observed", audit: audit.Comparison{Files: 8, Verified: false},
			want: []string{"no concurrent changes observed across 8 files"},
		},
		{
			name: "concurrent files", observation: "observed", audit: audit.Comparison{Files: 8, ChangedFiles: 2},
			want: []string{"2 files changed during the run by another process; skuggsja does not write to source paths"},
		},
		{
			name: "directory only", observation: "observed", audit: audit.Comparison{Files: 8, DirectoryChanges: 2},
			want:   []string{"2 directory listings changed during the run by another process"},
			absent: []string{"2 files changed", "no concurrent changes"},
		},
		{
			name: "files and directories", observation: "observed", audit: audit.Comparison{Files: 8, ChangedFiles: 2, DirectoryChanges: 3},
			want: []string{"2 files changed", "3 directory listings changed"}, absent: []string{"5 files changed"},
		},
		{
			name: "observation disabled", observation: "disabled", skipped: true,
			want: []string{"observation skipped (--no-source-audit)"},
		},
		{
			name: "observation unavailable", observation: "unavailable",
			want: []string{"observation unavailable"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			printSummary(&out, app.Generation{Report: analytics.Report{Privacy: analytics.Privacy{
				SourceAccess: "read-only", SourceObservation: test.observation, SourceAudit: test.audit,
			}}}, test.skipped)
			text := out.String()
			for _, want := range append(test.want, "Source access          read-only; skuggsja does not write to source paths") {
				if !strings.Contains(text, want) {
					t.Errorf("summary missing %q:\n%s", want, text)
				}
			}
			for _, absent := range append(test.absent, "verified", "inconclusive", "warning", "Source files modified", "Source integrity") {
				if strings.Contains(text, absent) {
					t.Errorf("summary conflates access with observation using %q:\n%s", absent, text)
				}
			}
		})
	}
}
