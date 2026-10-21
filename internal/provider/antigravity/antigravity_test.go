package antigravity

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const schema = `
	CREATE TABLE conversation_summaries (
		conversation_id text PRIMARY KEY, title text NOT NULL DEFAULT '', preview text NOT NULL DEFAULT '',
		step_count integer NOT NULL DEFAULT 0, last_modified_time datetime NOT NULL,
		workspace_uris text NOT NULL, status text NOT NULL DEFAULT '', source text NOT NULL DEFAULT '',
		project_id text NOT NULL DEFAULT '', agent_name text NOT NULL DEFAULT '',
		parent_conversation_id text NOT NULL DEFAULT '', nesting_depth integer NOT NULL DEFAULT 0,
		last_user_input_time datetime NOT NULL, last_user_input_step_index integer NOT NULL DEFAULT -1
	);
	CREATE VIRTUAL TABLE conversations_fts USING fts5(
		conversation_id UNINDEXED, chunk_index UNINDEXED, chunk_first_step UNINDEXED, chunk_last_step UNINDEXED,
		user_step_byte_offsets UNINDEXED, title, user_body, agent_body
	);
`

func newFixture(t *testing.T, statements string) Reader {
	t.Helper()
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, summariesFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + statements); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return Reader{Root: root}
}

func read(t *testing.T, r Reader) (result struct {
	status   string
	sessions int
	warnings []string
}, prompts [][]int, ids []string, projects []string) {
	t.Helper()
	discovery, err := r.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := r.Read(context.Background(), discovery)
	result.status = out.Status
	result.sessions = len(out.Sessions)
	for _, w := range out.Warnings {
		result.warnings = append(result.warnings, w.Code)
	}
	for _, s := range out.Sessions {
		ids = append(ids, s.ID)
		projects = append(projects, s.Project)
		var sizes []int
		for _, p := range s.Prompts {
			sizes = append(sizes, p.Words)
		}
		prompts = append(prompts, sizes)
	}
	return
}

func TestPromptsComeFromTheUserStepIndexAndSliceTheirText(t *testing.T) {
	t.Parallel()
	body := "Add a health check endpoint please\nNow write a test\nShip it"
	// Byte offsets of the three user steps inside body.
	r := newFixture(t, `
		INSERT INTO conversation_summaries (conversation_id, last_modified_time, workspace_uris, last_user_input_time, last_user_input_step_index)
		VALUES ('c1', '2026-10-20 02:36:18.018431+00:00', '["file:///synthetic/agy-project"]', '2026-10-20 02:36:03.597285+00:00', 12);
		INSERT INTO conversations_fts (conversation_id, chunk_index, chunk_first_step, chunk_last_step, user_step_byte_offsets, title, user_body, agent_body)
		VALUES ('c1', 0, 0, 20, '[[0,0],[7,35],[12,52]]', 't', '`+body+`', 'agent text');
		INSERT INTO conversations_fts (conversation_id, chunk_index, chunk_first_step, chunk_last_step, user_step_byte_offsets, title, user_body, agent_body)
		VALUES ('c1', -1, 0, 0, '', '', '', '');
	`)
	got, prompts, ids, projects := read(t, r)
	if got.status != "supported" || got.sessions != 1 || ids[0] != "c1" || projects[0] != "agy-project" {
		t.Fatalf("unexpected result %#v ids=%v projects=%v", got, ids, projects)
	}
	if len(prompts[0]) != 3 || prompts[0][0] != 6 || prompts[0][1] != 4 || prompts[0][2] != 2 {
		t.Fatalf("prompt word counts = %v, want [6 4 2]", prompts[0])
	}
}

func TestOnlyTheLastUserInputCarriesATimeAndOthersStayUnknown(t *testing.T) {
	t.Parallel()
	r := newFixture(t, `
		INSERT INTO conversation_summaries (conversation_id, last_modified_time, workspace_uris, last_user_input_time, last_user_input_step_index)
		VALUES ('c1', '2026-10-20 02:36:18+00:00', '[]', '2026-10-20 02:36:03+00:00', 7);
		INSERT INTO conversations_fts (conversation_id, chunk_index, chunk_first_step, chunk_last_step, user_step_byte_offsets, title, user_body, agent_body)
		VALUES ('c1', 0, 0, 9, '[[0,0],[7,3]]', 't', 'one two', '');
	`)
	discovery, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), discovery)
	if len(out.Sessions) != 1 || len(out.Sessions[0].Prompts) != 2 {
		t.Fatalf("unexpected sessions: %#v", out.Sessions)
	}
	first, last := out.Sessions[0].Prompts[0], out.Sessions[0].Prompts[1]
	if !first.At.IsZero() {
		t.Fatalf("first prompt time was invented: %v", first.At)
	}
	if last.At.IsZero() || last.At.Format("15:04:05") != "02:36:03" {
		t.Fatalf("last user input lost its real time: %v", last.At)
	}
}

func TestConversationsWithoutIndexedUserStepsAreNotInvented(t *testing.T) {
	t.Parallel()
	r := newFixture(t, `
		INSERT INTO conversation_summaries (conversation_id, step_count, last_modified_time, workspace_uris, last_user_input_time, last_user_input_step_index)
		VALUES ('only-summary', 158, '2026-10-20 02:36:18+00:00', '[]', '2026-10-20 02:36:03+00:00', 156);
	`)
	got, _, _, _ := read(t, r)
	if got.sessions != 0 {
		t.Fatalf("step_count alone produced %d sessions; it is not a prompt record", got.sessions)
	}
}

func TestInvalidOffsetsKeepPromptsButDropSizes(t *testing.T) {
	t.Parallel()
	r := newFixture(t, `
		INSERT INTO conversation_summaries (conversation_id, last_modified_time, workspace_uris, last_user_input_time)
		VALUES ('c1', '2026-10-20 02:36:18+00:00', '[]', '2026-10-20 02:36:03+00:00');
		INSERT INTO conversations_fts (conversation_id, chunk_index, chunk_first_step, chunk_last_step, user_step_byte_offsets, title, user_body, agent_body)
		VALUES ('c1', 0, 0, 9, '[[0,0],[1,9999]]', 't', 'short', '');
	`)
	got, prompts, _, _ := read(t, r)
	if got.sessions != 1 || len(prompts[0]) != 2 || prompts[0][0] != 0 || prompts[0][1] != 0 {
		t.Fatalf("offsets beyond the body must not fabricate sizes: %#v %v", got, prompts)
	}
	if len(got.warnings) == 0 || got.warnings[0] != "prompt_text_slices_invalid" {
		t.Fatalf("missing warning: %v", got.warnings)
	}
}

func TestUnrecognizedSchemaIsReportedNotGuessed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, summariesFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE something_else (x integer)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	got, _, _, _ := read(t, Reader{Root: root})
	if got.status != "unsupported schema" {
		t.Fatalf("status = %q", got.status)
	}
}

func TestDiscoverKeepsRootProtectionWhenMissing(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "antigravity")
	discovery, err := (Reader{Root: root}).Discover(context.Background())
	if err != nil || len(discovery.Files) != 0 || len(discovery.ProtectedDirectories) != 1 {
		t.Fatalf("missing store lost protection: %#v err=%v", discovery, err)
	}
	empty, err := (Reader{}).Discover(context.Background())
	if err != nil || len(empty.ProtectedDirectories) != 0 || len(empty.ConfiguredFiles) != 0 {
		t.Fatalf("empty root protected unrelated paths: %#v err=%v", empty, err)
	}
	link := t.TempDir()
	target := filepath.Join(link, "real.db")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(link, summariesFile)); err == nil {
		if _, err := (Reader{Root: link}).Discover(context.Background()); err == nil {
			t.Fatal("symlinked summaries database must be refused")
		}
	}
}
