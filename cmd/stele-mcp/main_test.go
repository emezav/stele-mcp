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
