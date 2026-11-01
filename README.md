# stele-mcp

**The stele framework works completely without this tool.**

[stele](https://github.com/emezav/stele) is plain Markdown and needs no runtime. `stele-mcp` is an
optional, external companion: a single Go binary that serves, over MCP, tools that make it cheap
for an agent to verify what it writes into a stele instance. Every tool declares the shell command
it replaces, so you can drop it at any time without losing anything.

It lowers the cost of verifying; it does not replace verifying. Look at the result anyway.

## Tools

### `append_row`

Appends one row to the Markdown table that ends an append-only file: a session index, an effort
log, a letter index.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | File whose last content is the target table. Relative paths resolve against the server working directory. |
| `cells` | string list | required | One text per column, written verbatim. A literal pipe goes as `\|`. |
| `ascii_only` | bool | `false` | Reject any character outside 0x20-0x7E. |
| `dry_run` | bool | `false` | Validate and build the row without writing it. |

It fails, **without writing anything**, when:

- the file does not exist (it never creates one, so a mistyped path cannot create a stray file);
- the table is not the last content of the file, including a trailing blank line, because a row
  appended there would land outside the table;
- the table has no header and separator line;
- the number of cells differs from the header;
- a cell contains a line break or an unescaped `|`;
- `ascii_only` is set and a cell contains a non-ASCII character (the error names it as `U+XXXX`).

Text is written as given: `%` and any other character pass through untouched, since no `printf`
format is involved. The row keeps the file's line ending (LF or CRLF), and a missing final newline
is added first.

It returns the written line, its line number and the table header, so the agent can compare them
without rereading the file.

**Fallback.** The same checks in shell. The row is passed to `printf` as an *argument*, never as
the format string, so `%` is safe:

```bash
# usage: append_row FILE 'ROW' [ascii]
append_row() {
  local f=$1 row=$2 sep='^\|( *:?-+:? *\|)+ *$' n_head n_row
  cols() { awk -F'|' '{gsub(/\\\|/,""); print NF-2}'; }   # an escaped \| is not a column
  [ -f "$f" ] || { echo "no such file: $f"; return 1; }
  tail -n1 "$f" | grep -q '^ *|' || { echo "the table is not the last line of $f"; return 1; }
  n_head=$(grep -E -B1 "$sep" "$f" | grep -Ev "$sep" | grep '^|' | tail -1 | cols)
  n_row=$(printf '%s\n' "$row" | cols)
  [ "$n_head" = "$n_row" ] || { echo "columns: header $n_head, row $n_row"; return 1; }
  if [ "$3" = ascii ] && printf '%s\n' "$row" | LC_ALL=C grep -q '[^ -~]'; then
    echo "row has non-ASCII characters"; return 1
  fi
  printf '%s\n' "$row" >> "$f" && tail -3 "$f"
}
```

The fallback does not check line breaks or unescaped pipes inside cells, and it assumes the file
ends with a newline.

## Install

Requires Go 1.26 or later.

```bash
go install github.com/emezav/stele-mcp/cmd/stele-mcp@latest
```

## Use with an MCP client

The server speaks MCP over stdio. Start it from the root of the project that holds the stele
instance, so relative paths resolve there.

Claude Code:

```bash
claude mcp add stele-mcp -- stele-mcp
```

Any client that takes a JSON server list:

```json
{
  "mcpServers": {
    "stele-mcp": { "command": "stele-mcp" }
  }
}
```

## Development

```bash
go test ./...
go vet ./... && gofmt -l .
go build -o dist/ ./cmd/stele-mcp
```

The logic lives in `internal/` and does not import the MCP SDK; `cmd/stele-mcp` only registers the
tools and serves them. Built on the official
[MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## License

[MIT](LICENSE)
