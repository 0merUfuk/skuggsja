package provider

import "testing"

func TestProjectNameDropsEveryParentPath(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"/Users/synthetic/private/project-one":    "project-one",
		`C:\Users\synthetic\private\project-two`:  "project-two",
		"file:///synthetic/private/project-three": "project-three",
	} {
		if got := ProjectName(input); got != want {
			t.Errorf("ProjectName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeLabelRejectsAbsolutePathsButKeepsModelFamilies(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"/Users/synthetic/private/model", `C:\Users\synthetic\model`,
		`\\server\private\model`, "file:///synthetic/model",
	} {
		if got := SafeLabel(value, 100); got != "" {
			t.Errorf("SafeLabel(%q) = %q, want empty", value, got)
		}
	}
	if got := SafeLabel("openai/gpt-synthetic", 100); got != "openai/gpt-synthetic" {
		t.Fatalf("model family label = %q", got)
	}
}
