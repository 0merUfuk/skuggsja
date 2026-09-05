// Command serve-report is verification-only: it serves a retained real-data
// aggregate through the production handler without reading source histories again.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/app"
)

func main() {
	path := flag.String("report", "", "retained private aggregate JSON")
	flag.Parse()
	if *path == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: serve-report -report PRIVATE_AGGREGATE")
		os.Exit(2)
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read retained aggregate")
		os.Exit(1)
	}
	var report analytics.Report
	if err := json.Unmarshal(data, &report); err != nil || report.SchemaVersion != analytics.SchemaVersion {
		fmt.Fprintln(os.Stderr, "retained aggregate must use the current schema")
		os.Exit(1)
	}
	fmt.Printf("retained_report_sha256=%x schema_version=%d\n", sha256.Sum256(data), report.SchemaVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Serve(ctx, report, app.ServeOptions{Port: 0, Ready: func(url string) { fmt.Printf("url=%s\n", url) }}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
