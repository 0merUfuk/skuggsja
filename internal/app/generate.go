package app

import (
	"context"
	"fmt"
	"time"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

// GenerateOptions contains explicit seams for deterministic tests.
type GenerateOptions struct {
	Readers      []provider.Reader
	Location     *time.Location
	Now          func() time.Time
	OutputPath   string
	AuditSources bool
}

// Generation is the finished report plus measured operational evidence.
type Generation struct {
	Report     analytics.Report
	OutputPath string
	Duration   time.Duration
	AuditError error
}

// Generate discovers, snapshots, reads, aggregates, re-snapshots, and persists.
func Generate(ctx context.Context, options GenerateOptions) (Generation, error) {
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

	type discovered struct {
		reader    provider.Reader
		discovery provider.Discovery
		err       error
	}
	discoveries := make([]discovered, 0, len(options.Readers))
	var auditPaths []string
	var sourceRoots []string
	var sourceFiles []string
	for _, reader := range options.Readers {
		d, err := reader.Discover(ctx)
		discoveries = append(discoveries, discovered{reader: reader, discovery: d, err: err})
		auditPaths = append(auditPaths, d.Roots...)
		if err == nil {
			sourceRoots = append(sourceRoots, d.Roots...)
			auditPaths = append(auditPaths, d.Files...)
			sourceFiles = append(sourceFiles, d.Files...)
		}
	}
	if err := EnsureOutputSeparate(outputPath, sourceRoots, sourceFiles); err != nil {
		return Generation{}, err
	}

	var before audit.Snapshot
	var auditErr error
	if options.AuditSources && len(auditPaths) > 0 {
		before, auditErr = audit.Capture(ctx, auditPaths)
	}

	results := make([]model.ProviderResult, 0, len(discoveries))
	for _, item := range discoveries {
		if item.err != nil {
			result := model.ProviderResult{
				Harness: item.reader.Harness(), DisplayName: item.reader.DisplayName(),
				Status: "unavailable", SourceFiles: item.discovery.Files,
			}
			result.AddWarning("discovery_failed", "This provider's history location could not be inspected.")
			results = append(results, result)
			continue
		}
		results = append(results, item.reader.Read(ctx, item.discovery))
	}

	comparison := audit.Comparison{}
	if options.AuditSources && auditErr == nil && len(auditPaths) > 0 {
		after, err := audit.Capture(ctx, auditPaths)
		if err != nil {
			auditErr = err
		} else {
			comparison = audit.Compare(before, after)
		}
	}
	if options.AuditSources && auditErr != nil {
		results = append(results, model.ProviderResult{
			Harness: "system", DisplayName: "Source audit", Status: "unavailable",
			Warnings: []model.Warning{{Code: "source_audit_failed", Count: 1, Message: "Source integrity could not be fully audited for this run."}},
		})
	}

	report := analytics.Build(results, analytics.Options{
		Now: now(), Location: options.Location, SourceAudit: comparison,
	})
	if err := WriteReport(outputPath, report); err != nil {
		return Generation{}, fmt.Errorf("persist Rewind: %w", err)
	}
	return Generation{
		Report: report, OutputPath: outputPath, Duration: time.Since(started), AuditError: auditErr,
	}, nil
}
