// Package sum adds up a numeric column of the Markdown table that closes an
// append-only file, such as the hours of an effort log, without losing rows:
// a cell is a single value ("2") or a range ("3.25-4"), and any row that is
// neither stops the sum instead of counting as zero.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package sum

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/emezav/stele-mcp/internal/rows"
)

// Result is the sum of a column and the rows it covers.
type Result struct {
	Column  string  `json:"column"`
	Header  string  `json:"header"`
	Rows    int     `json:"rows"`
	Singles int     `json:"singles"`
	Ranges  int     `json:"ranges"`
	Low     float64 `json:"low"`
	High    float64 `json:"high"`
	Mid     float64 `json:"mid"`
}

const number = `(\d+(?:[.,]\d+)?)`

var (
	singleRe = regexp.MustCompile(`^` + number + `$`)
	rangeRe  = regexp.MustCompile(`^` + number + ` *- *` + number + `$`)
)

// maxListed caps how many offending rows an error names.
const maxListed = 10

// Column sums the column named column (case-insensitive) of the table that
// ends the file at path. Trailing blank lines are allowed; any other content
// after the table is an error. Every data row must hold a single value or a
// range in that column, or nothing is summed: a row read as zero is the
// failure this exists to prevent.
func Column(path, column string) (Result, error) {
	lines, start, last, err := rows.ReadClosingTable(path)
	if err != nil {
		return Result{}, err
	}

	header := rows.SplitCells(lines[start])
	col, err := rows.FindColumn(header, column)
	if err != nil {
		return Result{}, err
	}
	res := Result{Column: strings.TrimSpace(header[col]), Header: lines[start]}

	low, high := new(big.Rat), new(big.Rat)
	var bad []string
	for i := start + 2; i <= last; i++ {
		res.Rows++
		cells := rows.SplitCells(lines[i])
		if len(cells) != len(header) {
			bad = append(bad, fmt.Sprintf("line %d has %d cells, header has %d", i+1, len(cells), len(header)))
			continue
		}
		cell := strings.TrimSpace(cells[col])
		lo, hi, err := parse(cell)
		if err != nil {
			bad = append(bad, fmt.Sprintf("line %d %q: %v", i+1, cell, err))
			continue
		}
		if lo == hi {
			res.Singles++
		} else {
			res.Ranges++
		}
		low.Add(low, lo)
		high.Add(high, hi)
	}
	if len(bad) > 0 {
		listed := bad
		if len(listed) > maxListed {
			listed = listed[:maxListed]
		}
		more := ""
		if len(bad) > maxListed {
			more = fmt.Sprintf("; and %d more", len(bad)-maxListed)
		}
		return Result{}, fmt.Errorf("%d of %d rows cannot be summed in column %q, so nothing was summed: %s%s",
			len(bad), res.Rows, res.Column, strings.Join(listed, "; "), more)
	}

	mid := new(big.Rat).Add(low, high)
	mid.Quo(mid, big.NewRat(2, 1))
	res.Low, _ = low.Float64()
	res.High, _ = high.Float64()
	res.Mid, _ = mid.Float64()
	return res, nil
}

// parse reads a cell as a single value or a low-high range. A decimal comma
// is accepted. Values are exact rationals, so sums of decimals do not drift.
func parse(cell string) (lo, hi *big.Rat, err error) {
	if m := singleRe.FindStringSubmatch(cell); m != nil {
		v := rat(m[1])
		return v, v, nil
	}
	if m := rangeRe.FindStringSubmatch(cell); m != nil {
		lo, hi = rat(m[1]), rat(m[2])
		if lo.Cmp(hi) > 0 {
			return nil, nil, errors.New("range runs backwards")
		}
		return lo, hi, nil
	}
	if cell == "" {
		return nil, nil, errors.New("empty")
	}
	return nil, nil, errors.New("neither a number nor a low-high range")
}

func rat(s string) *big.Rat {
	r, _ := new(big.Rat).SetString(strings.Replace(s, ",", ".", 1))
	return r
}
