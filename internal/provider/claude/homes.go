package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

// sourceReaders retains one logical provider across all verified config homes.
// Stored session-index paths are never used to construct a source reader.
func (r Reader) sourceReaders() []Reader {
	primary := r
	primary.ExtraHomes = nil
	sources := []Reader{primary}
	for _, home := range uniquePaths(r.ExtraHomes) {
		sources = append(sources, Reader{
			ProjectsDir: filepath.Join(home, "projects"), HistoryFile: filepath.Join(home, "history.jsonl"),
			StatsFile: filepath.Join(home, "stats-cache.json"), GlobalStateFile: filepath.Join(home, ".claude.json"),
		})
	}
	return sources
}

func (r Reader) Discover(ctx context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Claude}
	sources := r.sourceReaders()
	// Declare every source before any source inspection can fail. Extra homes
	// also guard new config backups and transcripts created during the audit.
	d.Roots = append(d.Roots, r.ExtraHomes...)
	for _, source := range sources {
		d.Roots = append(d.Roots, source.ProjectsDir, source.DesktopSessionsDir, source.CodeSessionsDir)
		d.ConfiguredFiles = append(d.ConfiguredFiles, source.HistoryFile, source.StatsFile, source.GlobalStateFile)
	}
	tidyDiscovery(&d)
	for _, home := range r.ExtraHomes {
		if !filepath.IsAbs(home) {
			return d, errors.New("Claude additional config home must be absolute")
		}
		info, err := os.Lstat(home)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return d, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return d, errors.New("Claude additional config home is a symbolic link")
		}
		if !info.IsDir() {
			return d, errors.New("Claude additional config home is not a directory")
		}
	}
	for _, source := range sources {
		found, err := source.discoverConfigured(ctx)
		d.Roots = append(d.Roots, found.Roots...)
		d.Files = append(d.Files, found.Files...)
		d.AuditFiles = append(d.AuditFiles, found.AuditFiles...)
		d.ConfiguredFiles = append(d.ConfiguredFiles, found.ConfiguredFiles...)
		tidyDiscovery(&d)
		if err != nil {
			return d, err
		}
	}
	return d, nil
}

func tidyDiscovery(d *provider.Discovery) {
	d.Roots = uniquePaths(d.Roots)
	d.Files = uniquePaths(d.Files)
	d.AuditFiles = uniquePaths(d.AuditFiles)
	d.ConfiguredFiles = uniquePaths(d.ConfiguredFiles)
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{})
	for _, path := range paths {
		if path != "" {
			seen[filepath.Clean(path)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (r Reader) supplementalPaths(d provider.Discovery, kind string) []string {
	var paths []string
	for _, path := range d.AuditFiles {
		for _, source := range r.sourceReaders() {
			match := false
			switch kind {
			case "history":
				match = path == source.HistoryFile
			case "stats":
				match = path == source.StatsFile
			case "state":
				match = isGlobalStateCandidate(source.GlobalStateFile, path)
			case "project-index":
				match = pathWithinRoot(source.ProjectsDir, path) && filepath.Base(path) == "sessions-index.json"
			}
			if match {
				paths = append(paths, path)
				break
			}
		}
	}
	return uniquePaths(paths)
}

func (r Reader) readHistorySources(ctx context.Context, d provider.Discovery, result *model.ProviderResult) []model.Session {
	var sessions []model.Session
	for _, path := range r.supplementalPaths(d, "history") {
		parsed, warnings, err := parseHistory(ctx, path)
		if err != nil {
			result.AddWarning("unreadable_history", "Claude Code's prompt-history index could not be read.")
			continue
		}
		for code, count := range warnings {
			addWarningCount(result, code, claudeWarningMessage(code), count)
		}
		// parseHistory uses a map; make ordering explicit before merging copies.
		sort.Slice(parsed, func(i, j int) bool { return parsed[i].ID < parsed[j].ID })
		sessions = append(sessions, parsed...)
	}
	return coalesceSessionCopies(sessions, result)
}

// coalesceSessionCopies unions partial copies of a physical session before
// analytics counts sessions, child sessions, or session-start rhythms. Call and
// prompt IDs remain available for the existing cross-session copied-history rule.
func coalesceSessionCopies(sessions []model.Session, result *model.ProviderResult) []model.Session {
	type identity struct {
		id, parent string
		child      bool
	}
	positions := make(map[identity]int)
	out := make([]model.Session, 0, len(sessions))
	for _, session := range sessions {
		key := identity{id: session.ID, child: session.IsChild}
		if session.IsChild {
			key.parent = session.ParentID
		}
		index, duplicate := positions[key]
		if !duplicate || session.ID == "" {
			positions[key] = len(out)
			out = append(out, session)
			continue
		}
		target := &out[index]
		result.AddWarning("copied_session_files_coalesced", "Additional files for the same logical session were combined; unique records and tools were retained without adding sessions or rhythm events.")
		targetValid := !target.Unanchored && !target.TimeUnavailable
		sourceValid := !session.Unanchored && !session.TimeUnavailable
		if sourceValid && !targetValid {
			target.StartedAt, target.EndedAt, target.ActivityAt = session.StartedAt, session.EndedAt, session.ActivityAt
			target.ActivityBasis, target.Project = session.ActivityBasis, session.Project
		} else if sourceValid == targetValid {
			target.StartedAt = earliestNonzero(target.StartedAt, session.StartedAt)
			if session.EndedAt.After(target.EndedAt) {
				target.EndedAt = session.EndedAt
			}
			target.ActivityAt = target.StartedAt
		}
		target.Unanchored = target.Unanchored && session.Unanchored
		target.TimeUnavailable = !sourceValid && !targetValid && (target.TimeUnavailable || session.TimeUnavailable)
		if target.Project == "" {
			target.Project = session.Project
		}
		prompts := make(map[string]struct{})
		for _, p := range target.Prompts {
			if p.EventID != "" {
				prompts[p.EventID] = struct{}{}
			}
		}
		for _, p := range session.Prompts {
			if _, duplicate := prompts[p.EventID]; p.EventID != "" && duplicate {
				result.AddWarning("copied_session_prompt_records", "Repeated prompt records in copies of one session were retained once by stable event identity.")
				continue
			}
			if p.EventID != "" {
				prompts[p.EventID] = struct{}{}
			}
			target.Prompts = append(target.Prompts, p)
		}
		calls := make(map[string]int)
		for i, call := range target.Calls {
			if call.ID != "" {
				calls[call.ID] = i
			}
		}
		for _, call := range session.Calls {
			at, duplicate := calls[call.ID]
			if !duplicate || call.ID == "" {
				if call.ID != "" {
					calls[call.ID] = len(target.Calls)
				}
				target.Calls = append(target.Calls, call)
				continue
			}
			existing := &target.Calls[at]
			result.AddWarning("copied_session_response_records", "Repeated response records in copies of one session were combined by response ID; tool IDs were unioned.")
			modelConflict := existing.Model != "" && call.Model != "" && existing.Model != call.Model
			if call.Usage.Available && !modelConflict {
				if !existing.Usage.Available || cumulativeUsageAdvance(existing.Usage, call.Usage) {
					existing.Usage = call.Usage
				} else if existing.Usage != call.Usage && !cumulativeUsageAdvance(call.Usage, existing.Usage) {
					result.AddWarning("copied_session_usage_conflict", "Copies of one session disagree on non-cumulative response usage; one deterministic concrete source record was retained.")
				}
			}
			if existing.Model == "" {
				existing.Model = call.Model
			} else if call.Model != "" && existing.Model != call.Model {
				result.AddWarning("copied_session_model_conflict", "Copies of one session disagree on response model; the first deterministic source usage/model record was retained.")
			}
			tools := make(map[string]struct{})
			for _, id := range existing.ToolIDs {
				if id != "" {
					tools[id] = struct{}{}
				}
			}
			for _, id := range call.ToolIDs {
				if _, duplicate := tools[id]; id != "" && duplicate {
					continue
				}
				existing.ToolIDs = append(existing.ToolIDs, id)
				if id != "" {
					tools[id] = struct{}{}
				}
			}
		}
		target.Usage = model.TokenUsage{}
		target.Models = make(map[string]model.ModelActivity)
		target.ToolCalls = 0
		seenTools := make(map[string]struct{})
		for _, call := range target.Calls {
			target.Usage.Add(call.Usage)
			if call.Model != "" {
				activity := target.Models[call.Model]
				activity.Turns++
				activity.Usage.Add(call.Usage)
				target.Models[call.Model] = activity
			}
			for _, id := range call.ToolIDs {
				if _, seen := seenTools[id]; id == "" || !seen {
					target.ToolCalls++
					if id != "" {
						seenTools[id] = struct{}{}
					}
				}
			}
		}
		sort.SliceStable(target.Prompts, func(i, j int) bool {
			if target.Prompts[i].At.Equal(target.Prompts[j].At) {
				return strings.Compare(target.Prompts[i].EventID, target.Prompts[j].EventID) < 0
			}
			return target.Prompts[i].At.Before(target.Prompts[j].At)
		})
	}
	return out
}

func earliestNonzero(a, b time.Time) time.Time {
	if a.IsZero() || !b.IsZero() && b.Before(a) {
		return b
	}
	return a
}
