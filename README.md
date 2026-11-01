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

### `sweep`

Searches the Markdown under a path by **unit** (paragraph, list item, table row, code block...)
instead of by line, through [markgrep](https://github.com/emezav/markgrep)'s `search` package. A
phrase split by a line break is still found, and the pattern never goes through a shell, so its
backslashes arrive as written. It does not read `.gitignore`: an ignored folder is searched, not
silently skipped.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | Markdown file or directory. Relative paths resolve against the server working directory. |
| `patterns` | string list | required | Phrasings of the same idea; a unit matches if any hits. RE2 regular expressions. |
| `literal` | bool | `false` | Patterns and controls are fixed strings. |
| `case_sensitive` | bool | `false` | Match case. The default is case-insensitive. |
| `invert` | bool | `false` | Return the units where no pattern hits. |
| `kind` | string | all | Comma-separated unit kinds: `paragraph`, `item`, `code`, `table`, `heading`, `quote`, `frontmatter`, `html`. |
| `links` | string | `text` | What a `[text](url)` link matches as: `text`, `url` or `both`. |
| `include` | string list | none | Base-name globs; a file is searched only if it matches one. |
| `exclude` | string list | none | Base-name globs; a file matching any is skipped. |
| `control` | string list | none | Phrases known to be in the corpus. |
| `limit` | int | `100` | Maximum matches returned. |

It fails, **reporting nothing**, when:

- the path does not exist, or no pattern is given;
- a pattern, a kind or a link mode is invalid, or a glob is malformed;
- an `include` or `exclude` glob matches no file (a filter that filters nothing reads exactly like
  one that worked);
- the filters together leave no file, or the path holds no Markdown (zero matches over zero files
  reads like a clean corpus);
- a `control` phrase matches nothing: a search that cannot find a phrase known to be there cannot
  be trusted to say anything is absent. Build a negative control at run time, not as a literal you
  will later write down, or the corpus will end up containing it.

It returns `root`, the `files` searched after the filters, the `units` in them, `matched` (every
match), `truncated`, the `matches` (up to `limit`, each with `path`, `line`, `kind`, `heading`,
`parent`, `text` and the `patterns` that hit) and `grep`, one equivalent `grep` command per pattern.
Paths are relative to `root`, with forward slashes. `text` is the unit as written; matching runs on
its flattened text, with inline emphasis removed. Check `files` against the corpus you meant to
search.

**Fallback.** The `markgrep` CLI, with the same options:

```bash
markgrep search -F -e 'phrase' --in bitacora --exclude memory.md --control 'known phrase'
```

It prints the corpus first; on an empty corpus it exits 1, like a search with no matches, so read
the `corpus:` line. Without `markgrep`, the `grep` commands that `sweep` returns, which are
line-based and miss a phrase split by a line break.

### `check_index`

Cross-checks the Markdown table that ends an append-only index (the session index, the letter index)
against the files it lists, named `<prefix>-<number>-<YYYY-MM-DD>.md` in its folder. A row whose file
was never written, or a file whose row was never appended, leaves both sides looking fine on their
own: each one only shows up against the other.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | Index file whose last content is the table. Trailing blank lines are allowed. Relative paths resolve against the server working directory. |
| `prefix` | string | required | File name prefix, such as `sesion` or `carta`. |
| `dir` | string | the index's folder | Folder holding the files. |
| `number_column` | string | the first column | Header name of the number column, case-insensitive. |
| `date_column` | string | none | Header name of a date column to compare with the date in each file name. Without it, dates are not compared. |

It reports, and does not judge: a row without a file can be a documented absence, so read each list.
It returns `rows`, `files` (well-formed names only), `dir`, `header`, `ok` (true when every list is
empty) and these lists:

- `missing_files`: rows whose number has no file (`number`, `line`);
- `unindexed`: files whose number has no row;
- `duplicate_rows`, `duplicate_files`: every row or file sharing its number with another (`001` and
  `1` are the same number);
- `date_mismatches`: with `date_column`, rows whose date differs from their file name (`number`,
  `line`, `row_date`, `file`); a number with two files is reported as a duplicate and not compared;
- `malformed`: `prefix-*` names without the expected shape;
- `gaps`: numbers between the lowest and the highest that are in neither the index nor the folder.

It fails, **checking nothing**, when:

- the prefix is empty, or the file or the folder does not exist;
- a table is not the last content of the file, or has no header and separator line;
- a column named in the input is not in the header (the error lists the header), or appears twice;
- any row has a different cell count than the header, or a number cell that is not a number. The
  error names the first ten such rows by line number;
- no file in the folder carries the prefix, which is a wrong prefix or folder far more often than an
  empty index.

**Fallback.** The core of the same check in `bash`:

```bash
# usage: check_index INDEX PREFIX [DIR]
check_index() {
  local dir=${3:-$(dirname "$1")} rows files
  rows=$(awk -F'|' '
    !/^ *\|/ { if (NF && n) after = 1; t = 0; next }    # any other line ends a table
    !t { t = 1; after = n = 0; next }                    # header of a new table
    ++n > 1 { c = $2; gsub(/ /, "", c); r[n] = c }       # first column; skips the separator
    END { if (after) { print "text after the table" > "/dev/stderr"; exit 1 }
          for (i = 2; i <= n; i++) print r[i] }' "$1") || return 1
  if [ -n "$rows" ] && grep -qv '^[0-9][0-9]*$' <<<"$rows"; then
    grep -v '^[0-9][0-9]*$' <<<"$rows" | sed 's/^/not a number: /'; return 1
  fi
  rows=$(sed 's/^0*\(.\)/\1/' <<<"$rows" | sort -n)
  files=$(ls "$dir" | sed -n "s/^$2-0*\([0-9][0-9]*\)-[0-9]\{4\}-[0-9][0-9]-[0-9][0-9]\.md$/\1/p" | sort -n)
  [ -n "$files" ] || { echo "no file named $2-NNN-YYYY-MM-DD.md in $dir"; return 1; }
  echo "rows $(grep -c . <<<"$rows")  files $(grep -c . <<<"$files")"
  comm -23 <(grep . <<<"$rows" | sort -u) <(sort -u <<<"$files") | sort -n | sed 's/^/row without file: /'
  comm -13 <(grep . <<<"$rows" | sort -u) <(sort -u <<<"$files") | sort -n | sed 's/^/file without row: /'
  uniq -d <<<"$rows" | sed 's/^/repeated row: /'
  uniq -d <<<"$files" | sed 's/^/repeated file: /'
}
```

The fallback reads the number from the first column only, and does not compare dates, list malformed
names or gaps, or check cell counts.

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
