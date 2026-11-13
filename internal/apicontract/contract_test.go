// Package apicontract checks the public interfaces against each other: the
// UI only calls methods that exist, and docs/API.md names every bound method,
// event and MCP tool. It reads source files, so it runs on any platform.
package apicontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const root = "../.."

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func glob(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("no files for %s: %v", pattern, err)
	}
	return files
}

// boundMethods returns the exported methods of App, which Wails binds.
func boundMethods(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range glob(t, "*.go") {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range parsed.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() {
				continue
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" {
					out = append(out, fn.Name.Name)
				}
			}
		}
	}
	return out
}

func TestUICallsOnlyExistingMethods(t *testing.T) {
	bound := boundMethods(t)
	call := regexp.MustCompile(`\bapi\.([A-Z]\w+)`)
	for _, f := range glob(t, "frontend/dist/js/*.js") {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllStringSubmatch(string(raw), -1) {
			if !slices.Contains(bound, m[1]) {
				t.Errorf("%s calls api.%s, which App does not have", filepath.Base(f), m[1])
			}
		}
	}
}

func TestDocsNameEveryMethod(t *testing.T) {
	doc := read(t, "docs/API.md")
	for _, m := range boundMethods(t) {
		if !strings.Contains(doc, "`"+m+"`") {
			t.Errorf("docs/API.md does not describe the method %s", m)
		}
	}
}

func TestDocsNameEveryMCPTool(t *testing.T) {
	doc := read(t, "docs/API.md")
	tool := regexp.MustCompile(`mcp\.Tool\{Name: "(\w+)"`)
	found := tool.FindAllStringSubmatch(read(t, "internal/mcpserver/server.go"), -1)
	if len(found) == 0 {
		t.Fatal("no MCP tools found, did the source change?")
	}
	for _, m := range found {
		if !strings.Contains(doc, "`"+m[1]+"`") {
			t.Errorf("docs/API.md does not describe the MCP tool %s", m[1])
		}
	}
}

func TestDocsNameEveryEvent(t *testing.T) {
	doc := read(t, "docs/API.md")
	event := regexp.MustCompile(`(?:Event\w*\s*=\s*|emit\()"([a-z]+:[a-z:]+)"`)
	files := append(glob(t, "*.go"), glob(t, "internal/terminal/*.go")...)
	seen := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range event.FindAllStringSubmatch(string(raw), -1) {
			seen++
			if !strings.Contains(doc, "`"+m[1]) {
				t.Errorf("docs/API.md does not describe the event %s", m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no events found, did the source change?")
	}
}
