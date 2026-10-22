package gtfs

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// table streams one GTFS CSV file. Columns are addressed by header name,
// never position; every field is whitespace-trimmed on access.
type table struct {
	name string
	rc   io.ReadCloser
	r    *csv.Reader
	cols map[string]int
	rec  []string
	bad  int // rows the CSV layer could not parse
}

// openTable opens name inside the zip and reads its header. It returns
// errMissingFile if the file is absent, and an error naming the column if any
// required column is missing.
func openTable(files map[string]*zip.File, name string, required ...string) (*table, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, errMissingFile)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	r := csv.NewReader(bufio.NewReaderSize(rc, 1<<16))
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		rc.Close()
		return nil, fmt.Errorf("read %s header: %w", name, err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		if i == 0 {
			h = strings.TrimPrefix(h, "\ufeff")
		}
		cols[strings.TrimSpace(h)] = i
	}
	for _, c := range required {
		if _, ok := cols[c]; !ok {
			rc.Close()
			return nil, fmt.Errorf("%s: missing required column %q", name, c)
		}
	}
	return &table{name: name, rc: rc, r: r, cols: cols}, nil
}

var errMissingFile = errors.New("file not in zip")

// col returns the index of a column, or -1 when the header lacks it.
func (t *table) col(name string) int {
	if i, ok := t.cols[name]; ok {
		return i
	}
	return -1
}

// next advances to the next record. It skips (and counts) records the CSV
// layer rejects, and returns false at EOF. Only I/O errors are returned.
func (t *table) next() (bool, error) {
	for {
		rec, err := t.r.Read()
		if err == nil {
			t.rec = rec
			return true, nil
		}
		if err == io.EOF {
			return false, nil
		}
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			t.bad++
			continue
		}
		return false, fmt.Errorf("read %s: %w", t.name, err)
	}
}

// get returns the trimmed field at column index i, or "" if the column is
// absent or the record is short. The result shares memory with the whole CSV
// line; strings.Clone it before retaining it in the index.
func (t *table) get(i int) string {
	if i < 0 || i >= len(t.rec) {
		return ""
	}
	return strings.TrimSpace(t.rec[i])
}

func (t *table) close() error { return t.rc.Close() }
