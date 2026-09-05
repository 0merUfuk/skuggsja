package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
)

// GenerateOptions contains explicit seams for deterministic tests.
type GenerateOptions struct {
	Readers      []provider.Reader
	Location     *time.Location
	Now          func() time.Time
	OutputPath   string
	AuditSources bool
	// TempParent optionally selects a private-workspace parent without changing
	// process environment. Its prospective child must be separate from sources.
	TempParent string
}

// Generation is the finished report plus measured operational evidence.
type Generation struct {
	Report     analytics.Report
	OutputPath string
	Duration   time.Duration
	AuditError error
}

type discoveredReader struct {
	reader    provider.Reader
	discovery provider.Discovery
	err       error
}

type discoveryInputs struct {
	items           []discoveredReader
	auditRoots      []string
	auditFiles      []string
	auditConfigured []string
	sourceRoots     []string
	sourceFiles     []string
}

// Generate discovers, snapshots, reads, aggregates, re-snapshots, and persists.
func Generate(ctx context.Context, options GenerateOptions) (generation Generation, returnErr error) {
	started := time.Now()
	now := options.Now
	if now == nil {
		now = time.Now
	}
	outputPath := options.OutputPath
	if outputPath == "" {
		var err error
		outputPath, err = DefaultOutputPath()
		if err != nil {
			return Generation{}, err
		}
	}

	inputs := discoverInputs(ctx, options.Readers)
	if err := EnsureOutputSeparate(outputPath, inputs.sourceRoots, inputs.sourceFiles); err != nil {
		return Generation{}, err
	}
	workspace, err := newPrivateWorkspace(options.TempParent, inputs.sourceRoots, inputs.sourceFiles)
	if err != nil {
		return Generation{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, workspace.close()) }()
	ctx = sqlitecopy.WithTempDir(ctx, workspace.directory)

	var before audit.Snapshot
	var auditErr error
	if options.AuditSources && inputs.hasAuditPaths() {
		stable := false
		for attempt := 0; attempt < 3; attempt++ {
			before, auditErr = audit.CaptureConfigured(ctx, inputs.auditRoots, inputs.auditFiles, inputs.auditConfigured)
			if auditErr != nil {
				break
			}
			refreshed := discoverInputs(ctx, options.Readers)
			if sameDiscoveryInputs(inputs, refreshed) {
				inputs = refreshed
				stable = true
				break
			}
			inputs = refreshed
			if err := EnsureOutputSeparate(outputPath, inputs.sourceRoots, inputs.sourceFiles); err != nil {
				return Generation{}, err
			}
			if err := workspace.ensureSeparate(inputs.sourceRoots, inputs.sourceFiles); err != nil {
				return Generation{}, err
			}
		}
		if !stable && auditErr == nil {
			auditErr = errors.New("source discovery did not stabilize before parsing")
		}
	}

	results := make([]model.ProviderResult, 0, len(inputs.items))
	for _, item := range inputs.items {
		if item.err != nil {
			result := model.ProviderResult{
				Harness: item.reader.Harness(), DisplayName: item.reader.DisplayName(),
				Status: "unavailable", SourceFiles: append(append([]string(nil), item.discovery.Files...), item.discovery.AuditFiles...),
			}
			result.AddWarning("discovery_failed", "This provider's history location could not be inspected.")
			results = append(results, result)
			continue
		}
		results = append(results, item.reader.Read(ctx, item.discovery))
	}

	comparison := audit.Comparison{}
	observationComplete := false
	if options.AuditSources && auditErr == nil && inputs.hasAuditPaths() {
		after, err := audit.CaptureConfigured(ctx, inputs.auditRoots, inputs.auditFiles, inputs.auditConfigured)
		if err != nil {
			auditErr = err
		} else {
			comparison = audit.Compare(before, after)
			observationComplete = before.Complete && after.Complete
		}
	}
	// Observation says whether other writers changed the source window. It is
	// independent of our read-only source access and never becomes a provider
	// failure or a warning suggesting that Skuggsja modified a source.
	observation := "disabled"
	if options.AuditSources {
		observation = "observed"
		if auditErr != nil || !inputs.hasAuditPaths() || !observationComplete {
			observation = "unavailable"
		}
	}

	report := analytics.Build(results, analytics.Options{
		Now: now(), Location: options.Location, SourceAudit: comparison, SourceObservation: observation,
	})
	if err := WriteReport(outputPath, report); err != nil {
		return Generation{}, fmt.Errorf("persist Rewind: %w", err)
	}
	return Generation{
		Report: report, OutputPath: outputPath, Duration: time.Since(started), AuditError: auditErr,
	}, nil
}

func discoverInputs(ctx context.Context, readers []provider.Reader) discoveryInputs {
	inputs := discoveryInputs{items: make([]discoveredReader, 0, len(readers))}
	for _, reader := range readers {
		discovery, err := reader.Discover(ctx)
		inputs.items = append(inputs.items, discoveredReader{reader: reader, discovery: discovery, err: err})
		inputs.auditRoots = append(inputs.auditRoots, discovery.Roots...)
		inputs.sourceRoots = append(inputs.sourceRoots, discovery.Roots...)
		inputs.sourceRoots = append(inputs.sourceRoots, discovery.ProtectedDirectories...)
		inputs.auditFiles = append(inputs.auditFiles, discovery.Files...)
		inputs.auditFiles = append(inputs.auditFiles, discovery.AuditFiles...)
		inputs.auditConfigured = append(inputs.auditConfigured, discovery.ConfiguredFiles...)
		inputs.sourceFiles = append(inputs.sourceFiles, discovery.Files...)
		inputs.sourceFiles = append(inputs.sourceFiles, discovery.AuditFiles...)
		inputs.sourceFiles = append(inputs.sourceFiles, discovery.ConfiguredFiles...)
	}
	return inputs
}

func (inputs discoveryInputs) hasAuditPaths() bool {
	return len(inputs.auditRoots)+len(inputs.auditFiles)+len(inputs.auditConfigured) > 0
}

func sameDiscoveryInputs(left, right discoveryInputs) bool {
	if len(left.items) != len(right.items) {
		return false
	}
	for index := range left.items {
		if left.items[index].reader.Harness() != right.items[index].reader.Harness() ||
			errorText(left.items[index].err) != errorText(right.items[index].err) ||
			!samePaths(left.items[index].discovery.Roots, right.items[index].discovery.Roots) ||
			!samePaths(left.items[index].discovery.ProtectedDirectories, right.items[index].discovery.ProtectedDirectories) ||
			!samePaths(left.items[index].discovery.Files, right.items[index].discovery.Files) ||
			!samePaths(left.items[index].discovery.AuditFiles, right.items[index].discovery.AuditFiles) ||
			!samePaths(left.items[index].discovery.ConfiguredFiles, right.items[index].discovery.ConfiguredFiles) {
			return false
		}
	}
	return true
}

func samePaths(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
