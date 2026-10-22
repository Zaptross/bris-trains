package gtfs

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
)

// formatVersion is bumped whenever Index's encoded shape changes, so a binary
// never trusts a gob written by an incompatible build.
const formatVersion = 1

type header struct {
	Magic   string
	Version int
}

const magic = "trains/gtfs-index"

// Encode writes ix as a versioned gob stream. Calling is omitted (it is
// about half the encoded size) and rebuilt by Decode.
func (ix *Index) Encode(w io.Writer) error {
	slim := *ix
	slim.Calling = nil
	bw := bufio.NewWriterSize(w, 1<<16)
	enc := gob.NewEncoder(bw)
	if err := enc.Encode(header{Magic: magic, Version: formatVersion}); err != nil {
		return fmt.Errorf("encode index header: %w", err)
	}
	if err := enc.Encode(&slim); err != nil {
		return fmt.Errorf("encode index: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("encode index: %w", err)
	}
	return nil
}

// Decode reads an index written by Encode. It rejects streams from another
// format version.
func Decode(r io.Reader) (*Index, error) {
	dec := gob.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	var h header
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("decode index header: %w", err)
	}
	if h.Magic != magic || h.Version != formatVersion {
		return nil, fmt.Errorf("decode index: unsupported format %q v%d (want v%d)", h.Magic, h.Version, formatVersion)
	}
	var ix Index
	if err := dec.Decode(&ix); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	ix.buildCalling()
	return &ix, nil
}
