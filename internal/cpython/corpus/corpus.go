package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Case is one corpus input and the CPython 3.14 outcome recorded for it.
// Src holds the source text directly; File names a source file in the CPython
// cache (slash-separated, relative to its root) whose bytes hash to SHA256.
type Case struct {
	ID     string `json:"id"`
	Origin string `json:"origin,omitempty"` // file:line in the CPython tree
	Mode   string `json:"mode"`             // exec, eval, single or tokenize
	Src    string `json:"src,omitempty"`
	File   string `json:"file,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Expect Expect `json:"expect,omitzero"`
}

// Expect is the recorded outcome. A Src case carries Dump; a File case
// carries Hash, the hex SHA-256 of what Dump would be. Error is set when the
// input is rejected at Stage ("parse", "compile", "tokenize" or "driver"); a
// "compile" error keeps the Dump of the tree that did parse.
type Expect struct {
	Dump     string       `json:"dump,omitempty"`
	Hash     string       `json:"hash,omitempty"`
	Error    *SyntaxError `json:"error,omitempty"`
	Stage    string       `json:"stage,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
}

// SyntaxError is the CPython exception raised for a rejected input. Type is
// the exception class name; the position fields are 1-based line and offset
// numbers as CPython reports them, zero when absent.
type SyntaxError struct {
	Type      string `json:"type"`
	Msg       string `json:"msg"`
	Lineno    int    `json:"lineno,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	EndLineno int    `json:"end_lineno,omitempty"`
	EndOffset int    `json:"end_offset,omitempty"`
}

// ErrDuplicate reports two cases with the same ID in one manifest.
var ErrDuplicate = errors.New("corpus: duplicate case id")

// Load reads a JSONL manifest.
func Load(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("corpus: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cases []Case
	seen := map[string]bool{}
	for {
		var c Case
		if err := dec.Decode(&c); err == io.EOF {
			return cases, nil
		} else if err != nil {
			return nil, fmt.Errorf("corpus: %s: %w", path, err)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("%w: %s in %s", ErrDuplicate, c.ID, path)
		}
		seen[c.ID] = true
		cases = append(cases, c)
	}
}

// Write stores cases as a JSONL manifest sorted by ID, creating parent
// directories. The encoding is deterministic: equal input gives equal bytes.
func Write(path string, cases []Case) error {
	sorted := slices.Clone(cases)
	slices.SortFunc(sorted, func(a, b Case) int { return strings.Compare(a.ID, b.ID) })

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for i, c := range sorted {
		if i > 0 && sorted[i-1].ID == c.ID {
			return fmt.Errorf("%w: %s", ErrDuplicate, c.ID)
		}
		if err := enc.Encode(c); err != nil {
			return fmt.Errorf("corpus: encode %s: %w", c.ID, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	return nil
}

// Source returns the case input: Src, or the bytes of File in the cache
// verified against SHA256. It fails with ErrNoCache when the cache is absent
// and ErrDrift when the file does not match.
func (c Case) Source() ([]byte, error) {
	if c.File == "" {
		return []byte(c.Src), nil
	}
	if !filepath.IsLocal(filepath.FromSlash(c.File)) {
		return nil, fmt.Errorf("corpus: %s: file %q escapes the cache", c.ID, c.File)
	}
	dir, ok := Dir()
	if !ok {
		return nil, ErrNoCache
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.File)))
	if err != nil {
		return nil, fmt.Errorf("corpus: %s: %w", c.ID, err)
	}
	sum := sha256.Sum256(data)
	want, err := hex.DecodeString(c.SHA256)
	if err != nil || !bytes.Equal(sum[:], want) {
		return nil, fmt.Errorf("%w: %s (%s)", ErrDrift, c.ID, c.File)
	}
	return data, nil
}
