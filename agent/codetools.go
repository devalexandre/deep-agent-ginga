package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
)

// CodeTool is the file toolkit the agents work with. It follows what coding
// agents on the market converged on: line-numbered reads in windows, edits
// by exact string replacement instead of rewriting whole files, content
// search and file globbing. Small edits cost a few output tokens and cannot
// truncate a file the way a full rewrite near the output limit can.
type CodeTool struct {
	toolkit.Toolkit
	workspace string
}

const (
	defaultReadLimit = 2000
	maxLineChars     = 2000
	maxGrepResults   = 200
	maxGlobResults   = 500
)

// Directories never worth searching.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, "target": true, ".next": true, ".idea": true,
}

type CodeReadParams struct {
	Path   string `json:"path" description:"File path, relative to the workspace or absolute" required:"true"`
	Offset int    `json:"offset,omitempty" description:"1-based line to start from. Default: 1"`
	Limit  int    `json:"limit,omitempty" description:"Maximum number of lines to return. Default: 2000"`
}

type CodeWriteParams struct {
	Path    string `json:"path" description:"File path, relative to the workspace or absolute" required:"true"`
	Content string `json:"content" description:"The complete file content" required:"true"`
}

type CodeEditParams struct {
	Path       string `json:"path" description:"File path, relative to the workspace or absolute" required:"true"`
	OldString  string `json:"old_string" description:"Exact text to replace, copied from the file without the line-number prefix. Include enough surrounding lines to make it unique" required:"true"`
	NewString  string `json:"new_string" description:"Text that replaces old_string" required:"true"`
	ReplaceAll bool   `json:"replace_all,omitempty" description:"Replace every occurrence instead of requiring exactly one. Default: false"`
}

type CodeGrepParams struct {
	Pattern    string `json:"pattern" description:"Regular expression to search for in file contents" required:"true"`
	Path       string `json:"path,omitempty" description:"File or directory to search. Default: the workspace"`
	Glob       string `json:"glob,omitempty" description:"Only search files whose path matches this glob, e.g. *.go or src/**/*.ts"`
	IgnoreCase bool   `json:"ignore_case,omitempty" description:"Case-insensitive search. Default: false"`
}

type CodeGlobParams struct {
	Pattern string `json:"pattern" description:"Glob such as **/*.go, cmd/*/main.go or *_test.go" required:"true"`
	Path    string `json:"path,omitempty" description:"Directory to search from. Default: the workspace"`
}

type CodeListParams struct {
	Path string `json:"path,omitempty" description:"Directory to list. Default: the workspace"`
}

// NewCodeTool builds the toolkit rooted at workspace: relative paths resolve
// against it.
func NewCodeTool(workspace string) *CodeTool {
	t := &CodeTool{workspace: workspace}
	t.Toolkit = toolkit.NewToolkit()
	t.Toolkit.Name = "Code"
	t.Toolkit.Description = "Read, search, create and edit files in the workspace."

	t.Toolkit.Register("ReadFile", "Read a text file. Lines come back numbered (\"  12\\tcode\"); the number and tab are not part of the file. Large files come back in windows: use offset and limit to read more.", t, t.ReadFile, CodeReadParams{})
	t.Toolkit.Register("EditFile", "Replace exact text in an existing file. Read the file first. old_string must match the file exactly (whitespace included) and be unique unless replace_all is true. Prefer this over WriteFile for changes to existing files.", t, t.EditFile, CodeEditParams{})
	t.Toolkit.Register("WriteFile", "Create a file, or replace a whole file, with the given content. Parent directories are created. For changes to an existing file use EditFile.", t, t.WriteFile, CodeWriteParams{})
	t.Toolkit.Register("Grep", "Search file contents with a regular expression. Returns path:line:text matches. Skips .git, node_modules, vendor and build output.", t, t.Grep, CodeGrepParams{})
	t.Toolkit.Register("Glob", "Find files by path pattern. ** matches any number of directories.", t, t.Glob, CodeGlobParams{})
	t.Toolkit.Register("ListDirectory", "List a directory. Directories end with /.", t, t.ListDirectory, CodeListParams{})
	return t
}

func (t *CodeTool) resolve(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return t.workspace
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workspace, path)
	}
	return filepath.Clean(path)
}

// display shows a path relative to the workspace when it is inside it.
func (t *CodeTool) display(path string) string {
	if rel, err := filepath.Rel(t.workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func (t *CodeTool) ReadFile(params CodeReadParams) (interface{}, error) {
	path := t.resolve(params.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", t.display(path), err)
	}
	if isBinary(data) {
		return nil, fmt.Errorf("%s is a binary file", t.display(path))
	}
	if len(data) == 0 {
		return fmt.Sprintf("%s is empty.", t.display(path)), nil
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	offset := params.Offset
	if offset < 1 {
		offset = 1
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}
	if offset > len(lines) {
		return nil, fmt.Errorf("%s has %d lines; offset %d is past the end", t.display(path), len(lines), offset)
	}
	end := offset - 1 + limit
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := offset - 1; i < end; i++ {
		line := lines[i]
		if len(line) > maxLineChars {
			line = line[:maxLineChars] + "... [line truncated]"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... %d more lines. Read on with offset=%d.\n", len(lines)-end, end+1)
	}
	return b.String(), nil
}

func (t *CodeTool) WriteFile(params CodeWriteParams) (interface{}, error) {
	path := t.resolve(params.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", t.display(path), err)
	}
	mode := os.FileMode(0o644)
	_, statErr := os.Stat(path)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(params.Content), mode); err != nil {
		return nil, fmt.Errorf("write %s: %w", t.display(path), err)
	}
	verb := "Created"
	if statErr == nil {
		verb = "Replaced"
	}
	return fmt.Sprintf("%s %s (%d lines).", verb, t.display(path), strings.Count(params.Content, "\n")+1), nil
}

func (t *CodeTool) EditFile(params CodeEditParams) (interface{}, error) {
	path := t.resolve(params.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", t.display(path), err)
	}
	if params.OldString == "" {
		return nil, fmt.Errorf("old_string is empty; use WriteFile to create a file")
	}
	if params.OldString == params.NewString {
		return nil, fmt.Errorf("old_string and new_string are the same; nothing to change")
	}
	content := string(data)
	count := strings.Count(content, params.OldString)
	switch {
	case count == 0:
		return nil, fmt.Errorf("old_string was not found in %s. Read the file again and copy the text exactly, without the line-number prefix", t.display(path))
	case count > 1 && !params.ReplaceAll:
		return nil, fmt.Errorf("old_string matches %d places in %s. Add surrounding lines to make it unique, or set replace_all", count, t.display(path))
	}

	firstAt := strings.Index(content, params.OldString)
	var updated string
	if params.ReplaceAll {
		updated = strings.ReplaceAll(content, params.OldString, params.NewString)
	} else {
		updated = strings.Replace(content, params.OldString, params.NewString, 1)
	}
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("write %s: %w", t.display(path), err)
	}

	// A few numbered lines around the first change, so the model sees the
	// result without reading the file again.
	startLine := strings.Count(updated[:firstAt], "\n") + 1
	changedLines := strings.Count(params.NewString, "\n") + 1
	lines := strings.Split(updated, "\n")
	from := max(startLine-3, 1)
	to := min(startLine+changedLines+2, len(lines))
	var b strings.Builder
	replacements := 1
	if params.ReplaceAll {
		replacements = count
	}
	fmt.Fprintf(&b, "Edited %s (%d replacement(s)). Lines %d-%d now:\n", t.display(path), replacements, from, to)
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i, lines[i-1])
	}
	return b.String(), nil
}

func (t *CodeTool) Grep(params CodeGrepParams) (interface{}, error) {
	root := t.resolve(params.Path)
	if rg, err := exec.LookPath("rg"); err == nil {
		return t.grepWithRipgrep(rg, root, params)
	}
	pattern := params.Pattern
	if params.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	var glob *regexp.Regexp
	if params.Glob != "" {
		glob = globRegexp(params.Glob)
	}

	var results []string
	truncated := false
	walkErr := walkFiles(root, func(path string) bool {
		if glob != nil && !glob.MatchString(t.relTo(root, path)) && !glob.MatchString(filepath.Base(path)) {
			return true
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return true
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if re.Match(scanner.Bytes()) {
				if len(results) >= maxGrepResults {
					truncated = true
					return false
				}
				results = append(results, fmt.Sprintf("%s:%d:%s", t.display(path), line, clip(scanner.Text(), 300)))
			}
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return grepAnswer(results, truncated), nil
}

func (t *CodeTool) grepWithRipgrep(rg, root string, params CodeGrepParams) (interface{}, error) {
	args := []string{"--line-number", "--no-heading", "--color=never", "--max-columns=300", "--max-columns-preview"}
	if params.IgnoreCase {
		args = append(args, "--ignore-case")
	}
	if params.Glob != "" {
		args = append(args, "--glob", params.Glob)
	}
	// The same directories the Go search skips, with or without a .gitignore.
	for _, dir := range sortedSkippedDirs() {
		args = append(args, "--glob", "!"+dir+"/")
	}
	args = append(args, "--regexp", params.Pattern, root)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = t.workspace
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches.", nil
		}
		return nil, fmt.Errorf("rg: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	var results []string
	truncated := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if len(results) >= maxGrepResults {
			truncated = true
			break
		}
		if strings.HasPrefix(line, root) {
			line = t.display(line[:strings.Index(line, ":")]) + line[strings.Index(line, ":"):]
		}
		results = append(results, line)
	}
	return grepAnswer(results, truncated), nil
}

func grepAnswer(results []string, truncated bool) string {
	if len(results) == 0 {
		return "No matches."
	}
	answer := strings.Join(results, "\n")
	if truncated {
		answer += fmt.Sprintf("\n... more than %d matches; narrow the pattern, path or glob.", maxGrepResults)
	}
	return answer
}

func (t *CodeTool) Glob(params CodeGlobParams) (interface{}, error) {
	root := t.resolve(params.Path)
	re := globRegexp(params.Pattern)
	var results []string
	truncated := false
	err := walkFiles(root, func(path string) bool {
		if re.MatchString(t.relTo(root, path)) {
			if len(results) >= maxGlobResults {
				truncated = true
				return false
			}
			results = append(results, t.display(path))
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return "No files match.", nil
	}
	sort.Strings(results)
	answer := strings.Join(results, "\n")
	if truncated {
		answer += fmt.Sprintf("\n... more than %d files; narrow the pattern.", maxGlobResults)
	}
	return answer, nil
}

func (t *CodeTool) ListDirectory(params CodeListParams) (interface{}, error) {
	dir := t.resolve(params.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", t.display(dir), err)
	}
	if len(entries) == 0 {
		return fmt.Sprintf("%s is empty.", t.display(dir)), nil
	}
	var b strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (t *CodeTool) relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// walkFiles visits regular files under root (or root itself when it is a
// file) until visit returns false.
func walkFiles(root string, visit func(path string) bool) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("%s: %w", root, err)
	}
	if !info.IsDir() {
		visit(root)
		return nil
	}
	stop := fmt.Errorf("stop")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !visit(path) {
			return stop
		}
		return nil
	})
	if err == stop {
		return nil
	}
	return err
}

func sortedSkippedDirs() []string {
	dirs := make([]string, 0, len(skippedDirs))
	for dir := range skippedDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

var globCache sync.Map

// globRegexp turns a glob into a regexp over slash-separated relative paths:
// ** spans directories, * and ? stay within one. A pattern without a slash
// matches the file name at any depth, as in .gitignore.
func globRegexp(pattern string) *regexp.Regexp {
	if cached, ok := globCache.Load(pattern); ok {
		return cached.(*regexp.Regexp)
	}
	p := filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	var b strings.Builder
	b.WriteString("^")
	if !strings.Contains(p, "/") {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			i++
			if i+1 < len(p) && p[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			end := strings.IndexByte(p[i:], '}')
			if end < 0 {
				b.WriteString(regexp.QuoteMeta(string(c)))
				continue
			}
			options := strings.Split(p[i+1:i+end], ",")
			for j := range options {
				options[j] = regexp.QuoteMeta(options[j])
			}
			b.WriteString("(?:" + strings.Join(options, "|") + ")")
			i += end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	globCache.Store(pattern, re)
	return re
}

func isBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
