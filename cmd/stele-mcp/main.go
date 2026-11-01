// Command stele-mcp is an optional MCP server, over stdio, with tools that
// make it cheap to verify what an agent writes into a stele instance.
// The stele framework works completely without it.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/emezav/stele-mcp/internal/ascii"
	"github.com/emezav/stele-mcp/internal/clock"
	"github.com/emezav/stele-mcp/internal/index"
	"github.com/emezav/stele-mcp/internal/rows"
	"github.com/emezav/stele-mcp/internal/sum"
	"github.com/emezav/stele-mcp/internal/sweep"
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

type sumColumnInput struct {
	Path   string `json:"path" jsonschema:"file whose last content is the table; trailing blank lines are allowed; relative paths resolve against the server working directory"`
	Column string `json:"column" jsonschema:"header name of the column to sum, case-insensitive"`
}

func sumColumn(_ context.Context, _ *mcp.CallToolRequest, in sumColumnInput) (*mcp.CallToolResult, sum.Result, error) {
	res, err := sum.Column(in.Path, in.Column)
	return nil, res, err
}

type sweepInput struct {
	Path          string   `json:"path" jsonschema:"Markdown file or directory to search; relative paths resolve against the server working directory; .gitignore is not read, so ignored folders are searched"`
	Patterns      []string `json:"patterns" jsonschema:"one or more phrasings; a unit matches if any of them hits; RE2 regular expressions unless literal is set"`
	Literal       bool     `json:"literal,omitempty" jsonschema:"treat patterns and controls as fixed strings (default false)"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" jsonschema:"match case (default false: case-insensitive)"`
	Invert        bool     `json:"invert,omitempty" jsonschema:"return the units where no pattern hits (default false)"`
	Kind          string   `json:"kind,omitempty" jsonschema:"comma-separated unit kinds to keep: paragraph, item, code, table, heading, quote, frontmatter, html; empty or any keeps all"`
	Links         string   `json:"links,omitempty" jsonschema:"what a [text](url) link matches as: text (default), url or both"`
	Include       []string `json:"include,omitempty" jsonschema:"base-name globs; a file is searched only if it matches one; each glob must match some file"`
	Exclude       []string `json:"exclude,omitempty" jsonschema:"base-name globs; a file matching any is skipped; each glob must match some file"`
	Control       []string `json:"control,omitempty" jsonschema:"phrases known to be in the corpus; if one matches nothing the search fails instead of reporting absence"`
	Limit         int      `json:"limit,omitempty" jsonschema:"maximum matches returned (default 100); matched always counts all of them"`
}

func sweepTool(_ context.Context, _ *mcp.CallToolRequest, in sweepInput) (*mcp.CallToolResult, sweep.Result, error) {
	res, err := sweep.Run(in.Path, in.Patterns, sweep.Options{
		Literal:       in.Literal,
		CaseSensitive: in.CaseSensitive,
		Invert:        in.Invert,
		Kind:          in.Kind,
		Links:         in.Links,
		Include:       in.Include,
		Exclude:       in.Exclude,
		Control:       in.Control,
		Limit:         in.Limit,
	})
	return nil, res, err
}

type checkIndexInput struct {
	Path         string `json:"path" jsonschema:"index file whose last content is the table, such as history/index.md or cartas.md; relative paths resolve against the server working directory"`
	Prefix       string `json:"prefix" jsonschema:"file name prefix of the listed files, such as sesion or carta; files are <prefix>-<number>-<YYYY-MM-DD>.md"`
	Dir          string `json:"dir,omitempty" jsonschema:"folder holding the files; default the folder of the index"`
	NumberColumn string `json:"number_column,omitempty" jsonschema:"header name of the number column, case-insensitive; default the first column"`
	DateColumn   string `json:"date_column,omitempty" jsonschema:"header name of a date column to compare with the date in each file name, such as Fecha; default no date check"`
}

func checkIndex(_ context.Context, _ *mcp.CallToolRequest, in checkIndexInput) (*mcp.CallToolResult, index.Result, error) {
	res, err := index.Check(in.Path, in.Prefix, index.Options{
		Dir:          in.Dir,
		NumberColumn: in.NumberColumn,
		DateColumn:   in.DateColumn,
	})
	return nil, res, err
}

type checkASCIIInput struct {
	Path         string   `json:"path" jsonschema:"file or directory to check, walked recursively without .git; relative paths resolve against the server working directory; .gitignore is not read, so ignored folders are checked"`
	Allow        string   `json:"allow,omitempty" jsonschema:"characters that are not reported, each one by itself, such as an em dash a project declares deliberate"`
	AllowSpanish bool     `json:"allow_spanish,omitempty" jsonschema:"also allow the precomposed Spanish letters and marks: acute vowels, u with diaeresis, n with tilde, both cases, and the inverted question and exclamation marks (default false)"`
	Include      []string `json:"include,omitempty" jsonschema:"base-name globs; a file is checked only if it matches one; each glob must match some file"`
	Exclude      []string `json:"exclude,omitempty" jsonschema:"base-name globs; a file matching any is skipped; each glob must match some file"`
	Limit        int      `json:"limit,omitempty" jsonschema:"maximum findings returned (default 100); found and chars always count all of them"`
}

func checkASCII(_ context.Context, _ *mcp.CallToolRequest, in checkASCIIInput) (*mcp.CallToolResult, ascii.Result, error) {
	res, err := ascii.Check(in.Path, ascii.Options{
		Allow:        in.Allow,
		AllowSpanish: in.AllowSpanish,
		Include:      in.Include,
		Exclude:      in.Exclude,
		Limit:        in.Limit,
	})
	return nil, res, err
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
	mcp.AddTool(server, &mcp.Tool{
		Name: "sum_column",
		Description: "Sum a numeric column of the Markdown table that ends a file, such as the hours of an " +
			"effort log. Each cell must be a single value (2) or a low-high range (3.25-4); a decimal comma " +
			"is accepted. Any row that is neither, or has a different cell count than the header, fails the " +
			"whole sum and is named in the error, instead of counting as zero. Returns rows, singles, ranges, " +
			"low, high and mid: check rows against the table you expect. " +
			"Fallback without this server: the awk script in the stele-mcp README.",
	}, sumColumn)
	mcp.AddTool(server, &mcp.Tool{
		Name: "sweep",
		Description: "Search the Markdown files under a path by unit (paragraph, list item, table, code block...), " +
			"not by line, so a phrase split by a line break is still found; the pattern never goes through a shell. " +
			"Does not read .gitignore. Fails instead of reporting zero when a filter matches no file, kind leaves " +
			"no unit of its type, no unit is left to search (no file, or only empty ones), or a control phrase " +
			"matches nothing. Returns the files searched, the unit count, " +
			"matched (all), the matches (up to limit, with truncated) and the equivalent grep commands. " +
			"Fallback without this server: markgrep search, or the grep commands it returns.",
	}, sweepTool)
	mcp.AddTool(server, &mcp.Tool{
		Name: "check_index",
		Description: "Cross-check the Markdown table that ends an append-only index (session index, letter index) " +
			"against the files <prefix>-<number>-<YYYY-MM-DD>.md in its folder. Reports rows without a file, files " +
			"without a row, repeated numbers in either, prefix-* names without that shape, numbers missing from both " +
			"(gaps), and, with date_column, rows whose date differs from their file name; ok is true when all are empty. " +
			"It does not judge: a row without a file can be a documented absence. Fails if a number cell is not a " +
			"number or no file carries the prefix. " +
			"Fallback without this server: the check_index shell function in the stele-mcp README.",
	}, checkIndex)
	mcp.AddTool(server, &mcp.Tool{
		Name: "check_ascii",
		Description: "List every character outside ASCII in a file or the files under a directory, with path, line, " +
			"column, code point (U+2014) and Unicode name (EM DASH), except the characters allowed. Nothing is allowed " +
			"by default; allow_spanish allows the precomposed Spanish letters and marks, and an accent written as a " +
			"separate combining mark is still reported. A byte that is not valid UTF-8 is reported with its hex value. " +
			"Files with a NUL byte are listed as binary and not read. Returns files, found (all), the findings (up to " +
			"limit, with truncated) and chars, one count per distinct character; ok is true when found is zero. Fails " +
			"instead of reporting zero when a glob matches no file or no text file is left to read. " +
			"Fallback without this server: the perl command in the stele-mcp README.",
	}, checkASCII)
	return server
}
