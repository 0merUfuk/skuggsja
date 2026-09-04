// Package cli defines Skuggsja's small, screenshot-friendly command surface.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/claude"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
	"github.com/0merUfuk/skuggsja/internal/provider/cursor"
	"github.com/0merUfuk/skuggsja/internal/provider/hermes"
	"github.com/spf13/cobra"
)

// New constructs the root command. Callers own the context and signal policy.
func New(version string) *cobra.Command {
	var (
		noOpen        bool
		once          bool
		jsonOutput    bool
		noSourceAudit bool
		port          int
	)
	command := &cobra.Command{
		Use:           "skuggsja",
		Short:         "A local-first Rewind for your AI coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if port < 0 || port > 65535 {
				return fmt.Errorf("port must be between 0 and 65535")
			}
			paths, err := platform.DefaultPaths()
			if err != nil {
				return err
			}
			applyPathOverrides(&paths)
			outputPath, err := app.DefaultOutputPath()
			if err != nil {
				return err
			}
			if !jsonOutput {
				fmt.Fprintln(cmd.OutOrStdout(), "\nSKUGGSJA  /  YOUR LOCAL AGENT REWIND")
				fmt.Fprintln(cmd.OutOrStdout(), "Reading local histories. Nothing leaves this machine.")
				fmt.Fprintln(cmd.OutOrStdout())
			}
			generation, err := app.Generate(cmd.Context(), app.GenerateOptions{
				Readers: readers(paths), Location: time.Local, OutputPath: outputPath,
				AuditSources: !noSourceAudit,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(generation.Report)
			}
			printSummary(cmd.OutOrStdout(), generation, noSourceAudit)
			if once {
				return nil
			}
			return app.Serve(cmd.Context(), generation.Report, app.ServeOptions{
				Port: port, OpenBrowser: !noOpen,
				Ready: func(url string) { fmt.Fprintf(cmd.OutOrStdout(), "\n→ %s\n", url) },
			})
		},
	}
	flags := command.Flags()
	flags.BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	flags.BoolVar(&once, "once", false, "generate the aggregate artifact and exit")
	flags.BoolVar(&jsonOutput, "json", false, "print the privacy-safe aggregate as JSON and exit")
	flags.BoolVar(&noSourceAudit, "no-source-audit", false, "skip before/after source hashing")
	flags.IntVar(&port, "port", 4321, "preferred localhost port (0 chooses any free port)")

	command.AddCommand(cleanCommand(), versionCommand(version))
	return command
}

func readers(paths platform.Paths) []provider.Reader {
	return []provider.Reader{
		claude.Reader{ProjectsDir: paths.ClaudeProjects},
		codex.Reader{SessionsDir: paths.CodexSessions, ArchivedDir: paths.CodexArchived},
		hermes.Reader{DatabasePath: paths.HermesDatabase},
		cursor.Reader{DatabasePath: paths.CursorStateDB},
	}
}

func applyPathOverrides(paths *platform.Paths) {
	overrides := []struct {
		name   string
		target *string
	}{
		{"SKUGGSJA_CLAUDE_PROJECTS", &paths.ClaudeProjects},
		{"SKUGGSJA_CODEX_SESSIONS", &paths.CodexSessions},
		{"SKUGGSJA_CODEX_ARCHIVED", &paths.CodexArchived},
		{"SKUGGSJA_HERMES_DATABASE", &paths.HermesDatabase},
		{"SKUGGSJA_CURSOR_DATABASE", &paths.CursorStateDB},
	}
	for _, override := range overrides {
		if value := os.Getenv(override.name); value != "" {
			*override.target = value
		}
	}
}

func printSummary(out io.Writer, generation app.Generation, auditSkipped bool) {
	report := generation.Report
	fmt.Fprintln(out, "Your Rewind is ready.")
	fmt.Fprintf(out, "Sessions analyzed      %s\n", formatInt(int64(report.Totals.Sessions)))
	fmt.Fprintf(out, "Human prompts          %s\n", formatInt(int64(report.Totals.Prompts)))
	fmt.Fprintf(out, "Coverage               %s\n", report.Coverage.Label)
	if auditSkipped {
		fmt.Fprintln(out, "Source integrity       not audited (--no-source-audit)")
	} else if report.Privacy.SourceAudit.Verified {
		fmt.Fprintf(out, "Source files modified  %d (verified across %s files)\n",
			report.Totals.SourceFilesChanged, formatInt(int64(report.Privacy.SourceAudit.Files)))
	} else {
		fmt.Fprintln(out, "Source integrity       inconclusive; see source audit in the Rewind")
	}
	fmt.Fprintf(out, "Generated artifact     %s\n", shortenHome(generation.OutputPath))
	fmt.Fprintf(out, "Elapsed                %s\n", generation.Duration.Round(time.Millisecond))
}

func cleanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Delete the fully regenerable Rewind artifact",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := app.DefaultOutputPath()
			if err != nil {
				return err
			}
			paths, err := platform.DefaultPaths()
			if err != nil {
				return err
			}
			applyPathOverrides(&paths)
			if err := app.EnsureOutputSeparate(path,
				[]string{paths.ClaudeProjects, paths.CodexSessions, paths.CodexArchived},
				append(sqliteGuardPaths(paths.HermesDatabase), sqliteGuardPaths(paths.CursorStateDB)...),
			); err != nil {
				return err
			}
			if err := app.Clean(path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", shortenHome(path))
			return nil
		},
	}
}

func sqliteGuardPaths(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
}

func versionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Skuggsja version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "skuggsja %s\n", version)
		},
	}
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if relative, relErr := filepath.Rel(home, path); relErr == nil && relative != "." && !strings.HasPrefix(relative, "..") {
			return filepath.Join("~", relative)
		}
	}
	return path
}

func formatInt(value int64) string {
	negative := value < 0
	if negative {
		value = -value
	}
	digits := strconv.FormatInt(value, 10)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// Execute is a small seam for main and black-box command tests.
func Execute(ctx context.Context, version string) error {
	return New(version).ExecuteContext(ctx)
}
