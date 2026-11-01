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

### `today`

Today's date, measured from the machine clock. The date in an agent's context is often UTC, so
in the last hours of a local evening it is already tomorrow; and `git log -1` gives the day of the
last commit, not today.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `dir` | string | server working directory | Git repository used to probe the time zone. |
| `commits` | int | `20` | How many recent commits to probe. |

It returns `date` (`YYYY-MM-DD`), `time`, `offset` (`-0500`), `zone`, and a `verdict` with an
`advice` sentence. The clock is only trustworthy if it runs where the project lives, so the tool
compares the author offset of each recent commit with the offset the local zone had **at that
commit's instant** (a daylight saving change is not a mismatch):

| Verdict | Meaning | What to do |
| --- | --- | --- |
| `agree` | Every probed commit matches, on an offset other than `+0000`. | Use the date. |
| `inconclusive` | They match, but on `+0000`: a UTC machine and a project that lives in UTC look the same. | Ask the user. |
| `disagree` | Some commits were stamped in another zone; they are listed in `mismatches`. | Ask the user. |
| `no_vcs` | No git, no repository or no commits. | Ask the user. |

A remote agent or a container usually runs in UTC and stamps its own commits in UTC, so clock and
commits fail together and agree. That is why matching on `+0000` is `inconclusive`, not `agree`.

**Fallback.** The clock and the probe in shell:

```bash
date '+%Y-%m-%d %z'                                      # today, and the zone of this shell
git log -20 --format='%ad' --date=format:'%z' | sort -u  # the zone of whoever commits
TZ=UTC date '+%z'                                        # must differ from the commits, or the probe proves nothing
```

If the offsets differ, ask the user. The fallback compares against today's offset, so across a
daylight saving change it reports a mismatch that the tool does not.

### `sum_column`

Sums a numeric column of the Markdown table that ends a file, such as the hours of an effort log.
A cell may be a single value (`2`) or a range (`3.25-4`), and a decimal comma is accepted. A summer
that only matches one of the two forms reads the other as zero: the total keeps a plausible shape
and is silently low.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | File whose last content is the table. Trailing blank lines are allowed. Relative paths resolve against the server working directory. |
| `column` | string | required | Header name of the column to sum, case-insensitive. |

It fails, **summing nothing**, when:

- the file does not exist, or a table is not its last content;
- the table has no header and separator line;
- the column is not in the header (the error lists the header), or appears twice;
- any row has a different cell count than the header, or a cell in the column that is neither a
  number nor a low-high range, including an empty one or a backwards range. The error names the
  first ten such rows by line number.

It returns `rows`, `singles`, `ranges`, `low`, `high` and `mid` (the midpoint of the two totals),
plus the column and the header. Values are added exactly, so `0.1 + 0.2` is `0.3`. Compare `rows`
with the number of rows you expect: it is the denominator of the sum.

**Fallback.** The same checks in `awk`:

```bash
# usage: sum_column FILE 'COLUMN'
sum_column() {
  awk -F'|' -v col="$2" '
    function num(s) { gsub(/,/, ".", s); return s + 0 }
    { gsub(/\\\|/, "") }                                  # an escaped \| is not a column
    !/^ *\|/ { if (NF) h = 0; next }                      # text after a table: it no longer closes the file
    !h { h = 1; k = n = lo = hi = bad = 0; w = NF          # header of a new table
         for (i = 2; i < NF; i++) { c = $i; gsub(/^ +| +$/, "", c)
           if (tolower(c) == tolower(col)) k = i }
         next }
    /^ *\|( *:?-+:? *\|)+ *$/ || !k { next }              # separator, or no such column
    { n++; c = $k; gsub(/^ +| +$/, "", c)
      if (NF != w) { print "line " NR ": cell count"; bad++ }
      else if (c ~ /^[0-9]+([.,][0-9]+)?$/) { lo += num(c); hi += num(c) }
      else if (c ~ /^[0-9]+([.,][0-9]+)? *- *[0-9]+([.,][0-9]+)?$/) {
        split(c, r, "-"); lo += num(r[1]); hi += num(r[2]) }
      else { print "line " NR ": \"" c "\""; bad++ } }
    END { if (!h) { print "no table closes the file"; exit 1 }
          if (!k) { print "no column " col; exit 1 }
          if (bad) { print bad " of " n " rows cannot be summed"; exit 1 }
          printf "rows %d  low %.2f  high %.2f  mid %.2f\n", n, lo, hi, (lo + hi) / 2 }
  ' "$1"
}
```

The fallback adds in floating point, does not detect a column that appears twice, and lists every
bad row rather than the first ten.

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
