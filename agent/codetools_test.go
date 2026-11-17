package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestCodeTool(t *testing.T, files map[string]string) (*CodeTool, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewCodeTool(dir), dir
}

func TestCodeReadFileNumbersLinesAndPages(t *testing.T) {
	tool, _ := newTestCodeTool(t, map[string]string{"a.txt": "one\ntwo\nthree\nfour\n"})

	out, err := tool.ReadFile(CodeReadParams{Path: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "     1\tone\n     2\ttwo\n     3\tthree\n     4\tfour\n" {
		t.Fatalf("read = %q", out)
	}

	out, err = tool.ReadFile(CodeReadParams{Path: "a.txt", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.(string), "     2\ttwo\n     3\tthree\n") || !strings.Contains(out.(string), "offset=4") {
		t.Fatalf("window = %q", out)
	}

	if _, err := tool.ReadFile(CodeReadParams{Path: "missing.txt"}); err == nil {
		t.Fatal("reading a missing file did not fail")
	}
}

func TestCodeEditFileReplacesExactUniqueText(t *testing.T) {
	tool, dir := newTestCodeTool(t, map[string]string{"main.go": "package main\n\nfunc a() int { return 1 }\nfunc b() int { return 1 }\n"})

	if _, err := tool.EditFile(CodeEditParams{Path: "main.go", OldString: "return 1", NewString: "return 2"}); err == nil || !strings.Contains(err.Error(), "matches 2 places") {
		t.Fatalf("ambiguous edit error = %v", err)
	}
	if _, err := tool.EditFile(CodeEditParams{Path: "main.go", OldString: "return 3", NewString: "return 2"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing text error = %v", err)
	}

	out, err := tool.EditFile(CodeEditParams{Path: "main.go", OldString: "func b() int { return 1 }", NewString: "func b() int { return 2 }"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "func b() int { return 2 }") {
		t.Fatalf("edit result does not show the change: %q", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(data) != "package main\n\nfunc a() int { return 1 }\nfunc b() int { return 2 }\n" {
		t.Fatalf("file = %q", data)
	}

	if _, err := tool.EditFile(CodeEditParams{Path: "main.go", OldString: "int", NewString: "int64", ReplaceAll: true}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Count(string(data), "int64") != 2 {
		t.Fatalf("replace_all left %q", data)
	}
}

func TestCodeWriteFileCreatesDirectories(t *testing.T) {
	tool, dir := newTestCodeTool(t, nil)
	out, err := tool.WriteFile(CodeWriteParams{Path: "pkg/x/new.go", Content: "package x\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.(string), "Created pkg/x/new.go") {
		t.Fatalf("write = %q", out)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "pkg/x/new.go")); string(data) != "package x\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestCodeGrepAndGlob(t *testing.T) {
	files := map[string]string{
		"calc/calc.go":        "package calc\n\nfunc Sum(v ...int) int { return 0 }\n",
		"calc/calc_test.go":   "package calc\n// Sum is tested here\n",
		"report/report.go":    "package report\n// uses calc.Sum\n",
		"node_modules/x/x.go": "func Sum() {}\n",
		"README.md":           "Sum things\n",
	}
	for _, withRipgrep := range []bool{true, false} {
		tool, _ := newTestCodeTool(t, files)
		if !withRipgrep {
			t.Setenv("PATH", "")
		}

		out, err := tool.Grep(CodeGrepParams{Pattern: `func Sum`})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.(string), "calc/calc.go:3:func Sum") || strings.Contains(out.(string), "node_modules") {
			t.Fatalf("grep (rg=%v) = %q", withRipgrep, out)
		}

		out, err = tool.Grep(CodeGrepParams{Pattern: `sum`, IgnoreCase: true, Glob: "*.go"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.(string), "README.md") || !strings.Contains(out.(string), "report/report.go:2:") {
			t.Fatalf("grep with glob (rg=%v) = %q", withRipgrep, out)
		}

		if out, _ := tool.Grep(CodeGrepParams{Pattern: "nothing-here"}); out != "No matches." {
			t.Fatalf("no match = %q", out)
		}
	}

	tool, _ := newTestCodeTool(t, files)
	for pattern, want := range map[string]string{
		"**/*_test.go": "calc/calc_test.go",
		"*.go":         "calc/calc.go\ncalc/calc_test.go\nreport/report.go",
		"calc/*.go":    "calc/calc.go\ncalc/calc_test.go",
		"*.{md,txt}":   "README.md",
	} {
		out, err := tool.Glob(CodeGlobParams{Pattern: pattern})
		if err != nil {
			t.Fatal(err)
		}
		if out != want {
			t.Errorf("glob %q = %q, want %q", pattern, out, want)
		}
	}
}

func TestPlanToolKeepsTheWholePlan(t *testing.T) {
	var seen []PlanStep
	plan := NewPlanTool(func(steps []PlanStep) { seen = steps })

	out, err := plan.Update(PlanUpdateParams{Steps: []string{"done: read calc.go", "doing: fix Divide", "todo: run go test"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Plan updated: 3 step(s), 2 left." {
		t.Fatalf("answer = %q", out)
	}
	if len(seen) != 3 || seen[1] != (PlanStep{Text: "fix Divide", Status: "doing"}) {
		t.Fatalf("onChange got %+v", seen)
	}
	if got := FormatPlan(plan.Steps()); got != "[x] read calc.go\n[>] fix Divide\n[ ] run go test\n" {
		t.Fatalf("plan = %q", got)
	}

	out, _ = plan.Update(PlanUpdateParams{Steps: []string{"todo: a", "todo: b"}})
	if !strings.Contains(out.(string), "exactly one step as doing") {
		t.Fatalf("no nudge for a plan without a doing step: %q", out)
	}
	if _, err := plan.Update(PlanUpdateParams{}); err == nil {
		t.Fatal("empty plan accepted")
	}
}

func TestToolsetIsSmallAndHasNoGitTools(t *testing.T) {
	clearRepoAIEnv(t)
	deep, err := NewDeepAgentWithConfig(DeepAgentConfig{ModelID: "ollama:llama3.1:8b", SkipModelCheck: true, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range deep.toolset {
		for method := range tool.GetMethods() {
			names = append(names, method)
		}
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{"Code_ReadFile", "Code_EditFile", "Code_WriteFile", "Code_Grep", "Code_Glob", "ShellTool_Execute"} {
		if !strings.Contains(joined, want) {
			t.Errorf("toolset lacks %s: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"Push", "Commit", "ListProcesses", "SystemInfo", "FileTool_"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("toolset still offers %s: %s", unwanted, joined)
		}
	}
	if len(names) > 8 {
		t.Errorf("toolset has %d tools: %s", len(names), joined)
	}
}
