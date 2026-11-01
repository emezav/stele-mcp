package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := newServer().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestAppendRowOverMCP(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index.md")
	if err := os.WriteFile(p, []byte("| N | Summary |\n| --- | --- |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connect(t)

	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "append_row",
		Arguments: map[string]any{"path": p, "cells": []string{"1", "95% done"}},
	})
	if err != nil || r.IsError {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
	if !strings.Contains(text(r), `"line_number":3`) {
		t.Fatalf("result = %s", text(r))
	}
	if b, _ := os.ReadFile(p); !strings.HasSuffix(string(b), "| 1 | 95% done |\n") {
		t.Fatalf("file = %q", b)
	}

	r, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "append_row",
		Arguments: map[string]any{"path": p, "cells": []string{"2"}},
	})
	if err != nil || !r.IsError || !strings.Contains(text(r), "header has 2, row has 1") {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
}

func TestTodayOverMCP(t *testing.T) {
	cs := connect(t)
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "today",
		Arguments: map[string]any{"dir": t.TempDir()},
	})
	if err != nil || r.IsError {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
	out, ok := r.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", r.StructuredContent)
	}
	if out["verdict"] != "no_vcs" || len(out["date"].(string)) != len("2006-01-02") {
		t.Fatalf("structured content = %v", out)
	}
}

func TestSumColumnOverMCP(t *testing.T) {
	p := filepath.Join(t.TempDir(), "effort.md")
	if err := os.WriteFile(p, []byte("| N | hours |\n| --- | --- |\n| 1 | 3-4 |\n| 2 | 2 |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connect(t)

	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "sum_column",
		Arguments: map[string]any{"path": p, "column": "hours"},
	})
	if err != nil || r.IsError {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
	out, ok := r.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", r.StructuredContent)
	}
	if out["rows"] != 2.0 || out["low"] != 5.0 || out["high"] != 6.0 || out["mid"] != 5.5 {
		t.Fatalf("structured content = %v", out)
	}

	r, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "sum_column",
		Arguments: map[string]any{"path": p, "column": "minutes"},
	})
	if err != nil || !r.IsError || !strings.Contains(text(r), `no column "minutes"`) {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
}

func TestSweepOverMCP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.md"), []byte("# memory\n\nA backslash \\d and a phrase\nsplit by a line break.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connect(t)

	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "sweep",
		Arguments: map[string]any{"path": dir, "patterns": []string{`\\d and a phrase split`}, "control": []string{"backslash"}},
	})
	if err != nil || r.IsError {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
	out, ok := r.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", r.StructuredContent)
	}
	if out["matched"] != 1.0 || out["truncated"] != false || len(out["files"].([]any)) != 1 {
		t.Fatalf("structured content = %v", out)
	}

	r, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "sweep",
		Arguments: map[string]any{"path": dir, "patterns": []string{"phrase"}, "control": []string{"not in the corpus"}},
	})
	if err != nil || !r.IsError || !strings.Contains(text(r), "positive control matched nothing") {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
}

func TestCheckIndexOverMCP(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cartas.md")
	if err := os.WriteFile(p, []byte("| # | Fecha |\n| --- | --- |\n| 001 | 2026-11-01 |\n| 002 | 2026-11-01 |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "carta-001-2026-11-01.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connect(t)

	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "check_index",
		Arguments: map[string]any{"path": p, "prefix": "carta", "date_column": "fecha"},
	})
	if err != nil || r.IsError {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
	out, ok := r.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", r.StructuredContent)
	}
	missing, _ := out["missing_files"].([]any)
	if out["ok"] != false || out["rows"] != 2.0 || out["files"] != 1.0 || len(missing) != 1 || len(out["gaps"].([]any)) != 0 {
		t.Fatalf("structured content = %v", out)
	}

	r, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "check_index",
		Arguments: map[string]any{"path": p, "prefix": "letter"},
	})
	if err != nil || !r.IsError || !strings.Contains(text(r), "no file named letter-*") {
		t.Fatalf("err = %v, result = %s", err, text(r))
	}
}
