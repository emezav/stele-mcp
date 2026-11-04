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
- an `include` or `exclude` glob matches no file, or `kind` leaves no unit of its type (a filter
  that filters nothing reads exactly like one that worked);
- no unit is left to search: the filters together leave no file, the path holds no Markdown, or its
  Markdown is empty (zero matches over zero units reads like a clean corpus);
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

It prints the corpus first, and since markgrep 0.13.0 an empty corpus or a dead filter exits 2, not
1 like a search with no matches; with an older markgrep, read the `corpus:` line. Without `markgrep`, the `grep` commands that `sweep` returns, which are
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

### `check_ascii`

Lists every character outside ASCII in a file or in the files under a directory, with where it is
and what it is. Nothing is allowed by default; the caller says what is, because a project may write
its docs with accents and its code, comments and commit messages in ASCII. It does not read
`.gitignore`: an ignored folder is checked, not silently skipped. `.git` is never read.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | File or directory, walked recursively. Relative paths resolve against the server working directory. |
| `allow` | string | none | Characters that are not reported, each one by itself, such as an em dash a project declares deliberate. |
| `allow_spanish` | bool | `false` | Also allow the precomposed Spanish letters and marks: acute vowels, u with diaeresis and n with tilde in both cases, and the inverted question and exclamation marks. |
| `include` | string list | none | Base-name globs; a file is checked only if it matches one. |
| `exclude` | string list | none | Base-name globs; a file matching any is skipped. |
| `limit` | int | `100` | Maximum findings returned. |

Each finding has `path`, `line`, `column` (in characters, from 1), `char`, `code` (`U+2014`) and
`name` (`EM DASH`). An accent typed as a separate combining mark (`U+0301`) looks like the
precomposed letter and is still reported, since it is not the character that was allowed. A byte
that is not valid UTF-8 is reported with an empty `char`, its hex value as `code` and
`invalid UTF-8 byte` as `name`.

It returns `root`, `files` (text files read), `allowed`, `ok` (true when nothing was found), `found`
(every finding), `truncated`, the `findings` (up to `limit`), `chars` (one count per distinct
character, most frequent first, always complete) and `binary`: files with a NUL byte, listed and not
read. Paths are relative to `root`, with forward slashes.

It fails, **reporting nothing**, when:

- the path does not exist, or a glob is malformed;
- an `include` or `exclude` glob matches no file (a filter that filters nothing reads exactly like
  one that worked);
- no text file is left to read: an empty folder, the filters leave nothing, or every file is binary
  (zero findings over zero files reads like a clean corpus).

**Fallback.** `perl` for the characters and their names, `grep` for invalid UTF-8; both come with
Git for Windows:

```bash
# usage: check_ascii PATH ['ALLOWED']
check_ascii() {
  find "$1" -name .git -prune -o -type f -print0 |
    ALLOWED=${2:-} xargs -0 perl -CSD -Mcharnames=:full -ne '
      BEGIN { $ok = $ENV{ALLOWED};
              @ARGV = grep { -B $_ ? !print "$_: binary, not read\n" : 1 } @ARGV }
      while (/([^\x00-\x7F])/g) {
        next if length $ok && $1 =~ /^[$ok]$/;
        printf "%s:%d:%d: U+%04X %s\n", $ARGV, $., pos, ord $1, charnames::viacode(ord $1);
      }
    } continue { close ARGV if eof'
  find "$1" -name .git -prune -o -type f -print0 |
    xargs -0 perl -e 'print "$_\0" for grep { !-B } @ARGV' |
    LC_ALL=C.UTF-8 xargs -0 grep -naxv '.*' /dev/null | cut -d: -f1,2 | sed 's/$/: invalid UTF-8/'
}
```

`ALLOWED` is the inside of a Perl character class; for `allow_spanish`, pass
`'\x{e1}\x{e9}\x{ed}\x{f3}\x{fa}\x{fc}\x{f1}\x{c1}\x{c9}\x{cd}\x{d3}\x{da}\x{dc}\x{d1}\x{bf}\x{a1}'`.
The fallback guesses binaries with Perl's `-B`, which also calls an empty file binary; it reports an
invalid line, not each invalid byte; and it does not count by character, apply globs, or fail on an
empty folder.

### `check_budgets`

Measures the documents of a stele instance against the line budgets of its manifest. A budget that
nobody measures does not fail when it is written: it fails by growing a little each session, and no
session notices. The tool reads the budgets and the role names from the manifest
(`stele.config.md`) instead of carrying its own, so a budget raised there is the one measured here.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `root` | string | the server working directory | Project root holding the instance. |
| `config` | string | `stele.config.md` under `root` | Manifest path. |

Sections are found **by position**, as the manifest's parsing contract says (Meta, Paths, Names,
Features, Budgets, Wording), not by their titles, which follow the project's language. A document's
path is `base`, then its container (`history_dir` for state, handover, index, audit and effort;
`correspondence_dir` for the letter index), then the name in the names table, or the kit's default
when the table has no row for it. Lines are counted as an editor shows them: a last line without a
newline counts.

It returns `config`, `section` (the title of the section it read, to check it is the budgets one),
`ok` (true when every budget holds), `budgets` (`role`, `file`, `lines`, `max`, `over`, `ok`,
`missing`, and the manifest `line` of the row) and `skipped`: the rows not measured, with the
`reason`. A row is skipped when its key is not a document role (the kit's own budgets, such as
`SKILL.md`), its budget is the sentinel, or the role is disabled in the names table. A document that
does not exist is reported as `missing`, and makes `ok` false.

It fails, **measuring nothing**, when:

- the manifest does not exist, or has fewer `##` sections than the contract names;
- a budget is not a whole number (the error gives its line in the manifest);
- no row measures a document role: zero budgets over zero documents reads like a clean instance.

**Fallback.** The instance's own protocol doc carries a budgets block for its own paths. This one
resolves the paths from the manifest, like the tool, in `awk`:

```bash
# usage: check_budgets [ROOT]
check_budgets() {
  local cfg=${1:-.}/stele.config.md
  [ -f "$cfg" ] || { echo "manifest does not exist: $cfg"; return 1; }
  awk -F'|' -v root="${1:-.}" '
    function trim(s) { gsub(/^[ `]+|[ `]+$/, "", s); return s }
    function off(v) { return v == "-" || v == "---" || v == "\342\200\224" }
    function name(r) { return (3, r) in val ? val[3, r] : def[r] }
    BEGIN {
      split("entry guia-agente.md gotchas memory.md charter design.md manual manual.md " \
            "protocol protocol.md specs requirements.md architecture architecture.md " \
            "state latest.md handover handover.md index index.md audit audit.md " \
            "effort esfuerzo.md correspondence cartas.md history_dir history/ " \
            "correspondence_dir correspondencia/", d, " ")
      for (i = 1; i in d; i += 2) def[d[i]] = d[i + 1]
      split("state handover index audit effort", h, " "); for (i in h) dir[h[i]] = "history_dir"
      dir["correspondence"] = "correspondence_dir"
    }
    /^(```|~~~)/ { fence = !fence; next }
    fence { next }
    /^## / { s++; t = 0; next }
    /^ *\|/ { if (t++ < 2) next
              k = trim($2); val[s, k] = trim($3)
              if (s == 5) { key[++n] = k; lim[n] = val[s, k]; at[n] = NR }
              next }
    { t = 0 }
    END {
      if (s < 6) { print cfg ": " s " ## sections, the contract names 6"; exit 1 }
      base = (2, "base") in val ? val[2, "base"] : "."; sub(/\/$/, "", base)
      for (i = 1; i <= n; i++) {
        k = key[i]
        if (!(k in def) || k ~ /_dir$/) { print "skip " k ": not a document role"; continue }
        if (off(lim[i])) { print "skip " k ": no budget"; continue }
        if (lim[i] !~ /^[0-9]+$/) { print "line " at[i] ": budget of " k " is not a whole number"; bad = 1; exit 1 }
        if (off(name(k)) || (k in dir && off(name(dir[k])))) { print "skip " k ": role disabled"; continue }
        f = base "/" (k in dir ? name(dir[k]) : "") name(k); sub(/^\.\//, "", f)
        c = 0; while ((r = getline line < (root "/" f)) > 0) c++
        close(root "/" f); m++
        if (r < 0) { printf "  %-14s %s MISSING\n", k, f; fail = 1; continue }
        l = lim[i] + 0; st = c <= l ? "OK" : "OVER by " c - l; if (c > l) fail = 1
        printf "  %-14s %4d / %-4d %s  %s\n", k, c, lim[i], st, f
      }
      if (!m) { print "no row measures a document role"; exit 1 }
      exit fail
    }' "$cfg"
}
```

The fallback reads every table of a section, not only the first, and when a budget is not a whole
number it fails after printing the rows before it.

### `check_comments`

Finds the HTML comments in Markdown files that do not do what they look like. HTML comments do not
nest: the first `-->` closes the comment, whatever depth it seems to have, so commented-out
scaffolding with a comment inside it, or with its closing sequence written as text, comes out live.
Rereading the source does not show it; rendering does, and a template is often never rendered by the
people who maintain it.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | required | Markdown file or directory, walked recursively without `.git`. Relative paths resolve against the server working directory. |
| `include` | string list | `*.md` for a directory | Base-name globs; a file is checked only if it matches one. |
| `exclude` | string list | none | Base-name globs; a file matching any is skipped. |
| `limit` | int | `100` | Maximum findings returned. |

Each finding has `path`, `line`, `column` (in characters, from 1), `detail` and one `kind`:

- `nested`: a `<!--` inside an open comment. The comment ends at the first `-->`, so what follows
  that close was meant hidden and is live. The detail names where the outer comment opened.
- `stray_close`: a `-->` outside any comment, usually the close the author meant for an outer
  comment, or the closing sequence written as text inside a comment that already ended.
- `unclosed`: a comment that never closes; everything after it is hidden. Reported where it opens.
- `abrupt`: `<!-->` or `<!--->`, which are complete empty comments, so what follows is live.

Fenced code blocks and inline code spans are not read: a marker written as an example in backticks is
not a finding. Inside a comment nothing is code, as in HTML: a fence line there is comment text.

It returns `root`, `files`, `comments` (comments that open and close), `ok` (true when nothing was
found), `found` (every finding), `truncated`, the `findings` (up to `limit`) and `binary` (files with a
NUL byte, listed and not read). Paths are relative to `root`, with forward slashes.

It fails, **reporting nothing**, when the path does not exist, a glob is malformed or matches no file,
or no text file is left to read.

**Fallback.** The same scanner in `perl`, over the files given:

```bash
# usage: check_comments FILE...   (for a folder, see below)
check_comments() {
  perl -CSD -ne '
    sub hit { printf "%s:%d:%d: %s\n", $ARGV, $_[0], $_[1] + 1, $_[2] }
    chomp(my $s = $_); $s =~ s/\r$//;
    if (!$in && length $fc) {
      $fc = "" if $s =~ /^ {0,3}(\Q$fc\E+)\s*$/ && length $1 >= $fl;
    } elsif (!$in && $s =~ /^ {0,3}(`{3,}(?!.*`)|~{3,})/) {
      ($fc, $fl) = (substr($1, 0, 1), length $1);
    } else {
      my $p = 0;
      while ($p < length $s) {
        if ($in) {
          my $e = index $s, "-->", $p; my $stop = $e < 0 ? length $s : $e;
          for (my $q = $p; ($q = index $s, "<!--", $q) >= 0 && $q < $stop; $q += 4) {
            hit($., $q, "nested (comment opened at line $ol)");
          }
          last if $e < 0;
          ($in, $p) = (0, $e + 3); next;
        }
        substr($s, $p) =~ /(`+|<!--|-->)/ or last;
        my ($at, $m) = ($p + $-[1], $1);
        if ($m =~ /^`/) {
          my $n = length $m;
          $p = substr($s, $at + $n) =~ /(?<!`)`{$n}(?!`)/ ? $at + $n + $+[0] : $at + $n;
        } elsif ($m eq "-->") {
          hit($., $at, "stray_close"); $p = $at + 3;
        } elsif (substr($s, $at + 4) =~ /^-?>/) {
          hit($., $at, "abrupt"); $p = $at + 4 + $+[0];
        } else {
          ($in, $ol, $oc, $p) = (1, $., $at, $at + 4);
        }
      }
    }
    if (eof) { hit($ol, $oc, "unclosed") if $in; ($in, $fc) = (0, ""); close ARGV }' "$@"
}
```

For a folder, export the function first (`export -f check_comments`), then:
`find DIR -name .git -prune -o -name '*.md' -print0 | xargs -0 bash -c 'check_comments "$@"' _`. The fallback does not count comments,
skip binaries or apply globs. Neither the tool nor the fallback knows indented code blocks or code
spans that cross a line: a marker in one of those is read as live.

### `check_handover`

Checks whether the handover of a stele instance is still current. The handover carries a seal: the
`HEAD` commit when the checkpoint was written. If it differs from today's `HEAD`, work happened after
the handover was written, and the tree, not the handover, says where things are. The comparison is
one line, and it is the only thing that tells a current checkpoint from an expired one.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `root` | string | the server working directory | Project root holding the instance. |
| `config` | string | `stele.config.md` under `root` | Manifest path, used to find the handover. |
| `path` | string | the manifest's handover role | Handover file, if not the one the manifest names. |
| `dir` | string | `root` | Git repository to compare with. |

The state is the first word of the `## Estado` section (`## State` and `## Status` are accepted too);
the seal is the first commit hash, 7 to 40 hex characters, of the `## Sello` (or `## Seal`) section,
with or without backticks. A later hash on the same section, such as the commit a long piece of work
opened on, is not the seal.

It returns `handover`, `state`, `active` (the state is not `SIN_TRABAJO_ACTIVO`), `seal` and its
`seal_line`, `head`, `resolved` (the seal as a full hash), `dirty` (uncommitted changes to versioned
files), `advice`, `ok` (true when the verdict is `current`) and the `verdict`:

- `current`: the seal is `HEAD`;
- `stale`: the seal is another commit. With `ancestor`, `behind` says how many commits came after it;
  without it, history was rewritten or the seal is on another branch;
- `unknown`: the seal is not a commit of this repository, or is ambiguous;
- `no_seal`: the section is missing or has no hash. Without version control, the kit asks for what is
  observed on disk instead, and that cannot be checked by a tool;
- `no_vcs`: `dir` is not a git repository.

It fails when the manifest is needed and does not exist, or the handover does not exist or has no
state.

**Fallback.** The same reading in `awk` and the comparison with `git`:

```bash
# usage: check_handover HANDOVER [REPO]
check_handover() {
  local state seal full head
  state=$(awk 'tolower($0) ~ /^## (estado|state|status) *$/ { s = 1; next }
               /^## / { s = 0 } s && NF { gsub(/[*`]/, ""); print $1; exit }' "$1")
  [ -n "$state" ] || { echo "no state in $1"; return 1; }
  seal=$(awk 'tolower($0) ~ /^## (sello|seal) *$/ { s = 1; next } /^## / { s = 0 }
              s && match($0, /[0-9a-fA-F]{7,40}/) { print substr($0, RSTART, RLENGTH); exit }' "$1")
  echo "state $state"
  head=$(git -C "${2:-.}" rev-parse HEAD) || { echo "verdict no_vcs"; return 1; }
  [ -n "$seal" ] || { echo "verdict no_seal"; return 1; }
  full=$(git -C "${2:-.}" rev-parse --verify --quiet "$seal^{commit}") || { echo "seal $seal  verdict unknown"; return 1; }
  if [ "$full" = "$head" ]; then echo "seal $seal  verdict current"; return 0; fi
  echo "seal $seal  verdict stale, HEAD is $(git -C "${2:-.}" rev-list --count "$full..HEAD") commit(s) after it"
  return 1
}
```

The fallback returns 0 only for `current`, and does not report `dirty` or ancestry.

### `check_cadence`

Computes the two reminders the close ritual of a stele instance writes in its state document: whether
an audit is due, and whether the kit is due a look at its origin. Neither triggers anything; each is a
comparison of two numbers, but the numbers live in three documents (the manifest, the session index
and the audit log), and that is where the comparison goes wrong.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `root` | string | the server working directory | Project root holding the instance. |
| `config` | string | `stele.config.md` under `root` | Manifest path. |
| `today` | string | the machine's local date | Date `YYYY-MM-DD` to count days to. |
| `session` | int | the highest number in the session index | Current session. At close time the index already has its row. |

- **Audit.** The last audit row's `Sesiones` column (the third) is the range it covered; its last
  number is the last session covered. `since` is the current session minus that one, and the audit
  is `overdue` when `since` is **more than** `audit_every_n_sessions`, as the kit words it: with
  session 10 covered and a cadence of 10, it is due when closing session 21. No audit log means never
  audited, counted from session 0. `off` when `audit_log` is off; `no_reminder` when the cadence is
  the sentinel.
- **Kit.** `days` is `today` minus `kit_revisado`, and the kit is `overdue` when `days` is more than
  `update_every_n_days`. `no_reminder` when `kit_origen` or `kit_revisado` is empty or the sentinel
  (a self-hosted kit has no origin), or the threshold is the sentinel.

A missing threshold takes the kit default (10 sessions, 30 days). It returns `config`, `today`, `ok`
(true when neither reminder is overdue), and `audit` (`status`, `every`, `log`, `audits`,
`last_covered`, `session`, `since`, `advice`) and `kit` (`status`, `origin`, `reviewed`, `every`,
`days`, `advice`). The advice says when the next one is due.

It fails when the manifest does not exist, `today` or `kit_revisado` is not `YYYY-MM-DD`, a threshold
is not a whole number, the session index has no session number, or the last audit row has no number
in its session range.

**Fallback.** The same reading in `awk`, `tail` and `date` (GNU `date -d`, as in Git for Windows):

```bash
# usage: check_cadence [ROOT] [YYYY-MM-DD]
check_cadence() {
  local root=${1:-.} today=${2:-$(date +%F)} base hist idx log every last covered n origin seen days
  [ -f "$root/stele.config.md" ] || { echo "manifest does not exist: $root/stele.config.md"; return 1; }
  val() {  # val SECTION_POSITION KEY: column 2 of the row, without spaces or backticks
    awk -F'|' -v n="$1" -v k="$2" '/^(```|~~~)/ { f = !f } f { next } /^## / { s++; next }
      s == n && /^ *\|/ { c = $2; gsub(/[ `]/, "", c); if (c == k) { v = $3; gsub(/^[ `]+|[ `]+$/, "", v); print v; exit } }' \
      "$root/stele.config.md"
  }
  base=$(val 2 base); base=${base:-.}; base=${base%/}
  hist=$(val 3 history_dir); hist=${hist:-history/}
  idx=$root/$base/$hist$(val 3 index); [ "$idx" = "$root/$base/$hist" ] && idx=${idx}index.md
  log=$root/$base/$hist$(val 3 audit); [ "$log" = "$root/$base/$hist" ] && log=${log}audit.md
  every=$(val 4 audit_every_n_sessions); every=${every:-10}
  last=$(awk -F'|' '/^ *\|/ { c = $2; gsub(/ /, "", c); if (c ~ /^[0-9]+$/ && c + 0 > m) m = c + 0 } END { print m + 0 }' "$idx")
  covered=0
  [ -f "$log" ] && covered=$(tail -n 1 "$log" | awk -F'|' '{ n = split($4, a, /[^0-9]+/); for (i = n; i; i--) if (a[i] != "") { print a[i] + 0; exit } }')
  if [ "$(val 4 audit_log)" = off ]; then echo "audit: off"
  else n=$((last - covered))
    [ "$n" -gt "$every" ] && echo "audit: OVERDUE, $n sessions since session $covered (every $every)" \
                          || echo "audit: ok, $n sessions since session $covered; due when closing $((covered + every + 1))"
  fi
  origin=$(val 1 kit_origen); seen=$(val 1 kit_revisado); every=$(val 4 update_every_n_days); every=${every:-30}
  case "$origin" in ''|-|---|$'\342\200\224') echo "kit: no kit_origen, no reminder"; return 0 ;; esac
  days=$(( ( $(date -d "$today" +%s) - $(date -d "$seen" +%s) ) / 86400 ))
  [ "$days" -gt "$every" ] && echo "kit: OVERDUE, $days days since $seen (every $every)" \
                           || echo "kit: ok, $days days since $seen (every $every)"
}
```

The fallback does not handle a sentinel threshold or a missing index, and does not check the formats.

### `next_session`

Composes the session record a stele close is about to write: the next number, the file name and the
exact paths the close writes and appends to. The close appends rows with `printf >>`, and an append to
a mistyped path does not fail: it creates the file it was meant to find, and the right one is left
without its row. It writes nothing.

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `root` | string | the server working directory | Project root holding the instance. |
| `config` | string | `stele.config.md` under `root` | Manifest path. |
| `today` | string | the machine's local date | Date `YYYY-MM-DD` for the file name. |

The number is one more than the highest of the session index (its first column) and of the record
files in the history folder. The file name comes from the manifest's `session` pattern, or the kit
default `sesion-{NNN}-{YYYY-MM-DD}.md`: the number is padded to as many digits as the token has `N`s.
Without `today`, the date comes from the machine clock with the [`today`](#today) probe, and
`date_verdict` and `date_advice` say whether to trust it; with it, `date_verdict` is `given`.

It returns `number`, `padded`, `date`, `date_verdict`, `date_advice`, `file`, `path`, `history_dir`,
`pattern`, `index`, `effort` (the effort log, when the role is on and the file exists), `index_last`,
`files_last`, `last_files` (the record files with the highest number), `advice` and `ok`: true when
the index and the folder end at the same number. When they do not, a close was left half done (a row
without its record, or a record without its row): run [`check_index`](#check_index) before writing.

It fails when the manifest does not exist, the session pattern has no `{NNN}` or `{YYYY-MM-DD}`
token, the index cannot be read or has a row whose first cell is not a number, or `today` is not
`YYYY-MM-DD`.

**Fallback.** The same composition in `bash`, `awk` and `sed`:

```bash
# usage: next_session [ROOT] [YYYY-MM-DD]
next_session() {
  local root=${1:-.} today=${2:-$(date +%F)} base dir pat idx tok a b n file
  [ -f "$root/stele.config.md" ] || { echo "manifest does not exist: $root/stele.config.md"; return 1; }
  val() {  # val SECTION_POSITION KEY: column 2 of the row, without spaces or backticks
    awk -F'|' -v n="$1" -v k="$2" '/^(```|~~~)/ { f = !f } f { next } /^## / { s++; next }
      s == n && /^ *\|/ { c = $2; gsub(/[ `]/, "", c); if (c == k) { v = $3; gsub(/^[ `]+|[ `]+$/, "", v); print v; exit } }' \
      "$root/stele.config.md"
  }
  base=$(val 2 base); base=${base:-.}; base=${base%/}
  dir=$(val 3 history_dir); dir=$base/${dir:-history/}
  pat=$(val 3 session); pat=${pat:-'sesion-{NNN}-{YYYY-MM-DD}.md'}
  idx=$(val 3 index); idx=$dir${idx:-index.md}
  tok=$(grep -o '{N*}' <<<"$pat") || { echo "no {NNN} token in $pat"; return 1; }
  a=$(awk -F'|' '/^ *\|/ { c = $2; gsub(/ /, "", c); if (c ~ /^[0-9]+$/ && c + 0 > m) m = c + 0 } END { print m + 0 }' "$root/$idx")
  b=$(ls "$root/$dir" | sed -n "s/^${pat%%"$tok"*}\([0-9][0-9]*\)-[0-9]\{4\}-[0-9][0-9]-[0-9][0-9]${pat##*\}}\$/\1/p" | sort -n | tail -n 1)
  b=$((10#${b:-0}))
  n=$(( (a > b ? a : b) + 1 ))
  file=${pat/"$tok"/$(printf '%0*d' $(( ${#tok} - 2 )) "$n")}; file=${file/'{YYYY-MM-DD}'/$today}
  echo "next $dir$file  (index ends at $a, folder at $b)"
  [ "$a" -eq "$b" ] || { echo "index and folder disagree: check_index before writing"; return 1; }
}
```

The fallback takes the date from `date +%F` without the time zone probe, and does not name the effort
log.

## Install

Requires Go 1.26 or later.

```bash
go install github.com/emezav/stele-mcp/cmd/stele-mcp@latest
```

`@latest` resolves to the highest release tag. Right after a release, the Go module proxy may still
serve the previous one for a while, and `go install` reports no error. Check what you got:

```bash
go version -m "$(go env GOPATH)/bin/stele-mcp"
```

The `mod` line names the version; compare it with the latest tag at
<https://github.com/emezav/stele-mcp/tags>. On Windows the file is `stele-mcp.exe`.

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
