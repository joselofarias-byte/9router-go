package freecoding

import (
	"bytes"
	"os"
	"path/filepath"
)

// WriteBundle stores runs.jsonl and report.md in dir.
func WriteBundle(dir string, recs []Record) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var raw bytes.Buffer
	if err := WriteJSONL(&raw, recs); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "runs.jsonl"), raw.Bytes(), 0o644); err != nil {
		return err
	}
	var md bytes.Buffer
	if err := WriteReport(&md, recs); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), md.Bytes(), 0o644)
}
