package export

import (
	"bufio"
	"io"
	"os"

	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

// Exporter serialises a record set. JSONL is the only implementation; a plain
// text renderer is the second.
type Exporter interface {
	Ext() string
	Write(w io.Writer, rd *logfile.Reader, metas []logfile.Meta) (int, error)
}

// JSONL writes the verbatim source lines, so an export reopens in the viewer.
type JSONL struct{}

func (JSONL) Ext() string { return ".jsonl" }

func (JSONL) Write(w io.Writer, rd *logfile.Reader, metas []logfile.Meta) (int, error) {
	bw := bufio.NewWriterSize(w, 1<<16)
	n := 0
	for _, m := range metas {
		line, err := rd.Line(m)
		if err != nil {
			return n, err
		}
		if _, err := bw.Write(line); err != nil {
			return n, err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return n, err
		}
		n++
	}
	return n, bw.Flush()
}

// ToFile writes metas to path. The file is created exclusively: an export can
// never overwrite a log.
func ToFile(path string, e Exporter, rd *logfile.Reader, metas []logfile.Meta) (int, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := e.Write(f, rd, metas)
	if err != nil {
		return n, err
	}
	return n, f.Sync()
}
