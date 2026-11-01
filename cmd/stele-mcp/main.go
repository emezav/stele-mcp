// Command stele-mcp is an optional MCP server, over stdio, with tools that
// make it cheap to verify what an agent writes into a stele instance.
// The stele framework works completely without it.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/emezav/stele-mcp/internal/clock"
	"github.com/emezav/stele-mcp/internal/rows"
)

var version = "dev"

type appendRowInput struct {
	Path      string   `json:"path" jsonschema:"file whose last content is the target table; relative paths resolve against the server working directory"`
	Cells     []string `json:"cells" jsonschema:"one text per column, written verbatim; a literal pipe must be escaped as \\|"`
	ASCIIOnly bool     `json:"ascii_only,omitempty" jsonschema:"reject any character outside 0x20-0x7E (default false)"`
	DryRun    bool     `json:"dry_run,omitempty" jsonschema:"validate and build the row without writing it (default false)"`
}

func appendRow(_ context.Context, _ *mcp.CallToolRequest, in appendRowInput) (*mcp.CallToolResult, rows.Result, error) {
	res, err := rows.Append(in.Path, in.Cells, rows.Options{ASCIIOnly: in.ASCIIOnly, DryRun: in.DryRun})
	return nil, res, err
}

type todayInput struct {
	Dir     string `json:"dir,omitempty" jsonschema:"git repository used to probe the time zone; default the server working directory"`
	Commits int    `json:"commits,omitempty" jsonschema:"how many recent commits to probe (default 20)"`
}

func today(_ context.Context, _ *mcp.CallToolRequest, in todayInput) (*mcp.CallToolResult, clock.Result, error) {
	return nil, clock.Today(in.Dir, in.Commits), nil
}

func main() {
	if err := newServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "stele-mcp", Version: version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "append_row",
		Description: "Append one row to the Markdown table that ends an append-only file. " +
			"Fails without writing if the file does not exist, the table is not the last content " +
			"of the file, the cell count differs from the header, or a cell has a line break or an " +
			"unescaped pipe. Returns the written line, its line number and the header. " +
			"Fallback without this server: the append_row shell function in the stele-mcp README.",
	}, appendRow)
	mcp.AddTool(server, &mcp.Tool{
		Name: "today",
		Description: "Today's date from the machine clock, not from the agent context, which is often UTC. " +
			"Probes the time zone of recent git commits, comparing each one with the local offset at " +
			"its own instant: verdict agree means the date can be trusted; inconclusive, disagree or " +
			"no_vcs mean ask the user for the date. " +
			"Fallback without this server: date '+%Y-%m-%d %z' and the git log probe in the stele-mcp README.",
	}, today)
	return server
}
