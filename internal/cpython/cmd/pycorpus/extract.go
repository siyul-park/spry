package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/siyul-park/spry/internal/cpython"
	"github.com/siyul-park/spry/internal/cpython/corpus"
)

// manifest is a JSONL corpus: extract.py selects the cases of group and
// driver records the outcome of each.
type manifest struct {
	path   string
	group  string
	driver string
}

// table is a JSON file printed verbatim by a driver.
type table struct {
	path   string
	driver string
	args   []string
	cache  bool // append the cache directory to args
}

type result struct {
	ID     string
	Expect corpus.Expect
}

const base = "testdata/cpython"

var manifests = []manifest{
	{"scanner/" + base + "/tokenize.jsonl", "tokenize", "tokens.py"},
	{"scanner/" + base + "/encoding.jsonl", "encoding", "parse.py"},
	{"parser/" + base + "/snippets.jsonl", "snippets", "parse.py"},
	{"parser/" + base + "/syntax.jsonl", "syntax", "parse.py"},
	{"parser/" + base + "/files.jsonl", "files", "parse.py"},
}

var tables = []table{
	{"token/" + base + "/tables.json", "tables.py", []string{"token"}, false},
	{"ast/" + base + "/schema.json", "tables.py", []string{"ast"}, false},
	{"scanner/" + base + "/unicode.json", "unicode.py", nil, false},
	{"ast/" + base + "/repr.jsonl", "repr.py", nil, true},
	{"types/" + base + "/typerepr.jsonl", "typerepr.py", nil, false},
}

func extract(ctx context.Context, cache string) error {
	python, err := cpython.Lookup()
	if err != nil {
		return err
	}
	actual, err := cpython.Check(ctx, python)
	if err != nil {
		return err
	}
	if actual != cpython.Version {
		fmt.Fprintf(os.Stderr, "warning: python %s differs from pinned %s; output may drift\n", actual, cpython.Version)
	}
	for _, t := range tables {
		args := t.args
		if t.cache {
			args = append(args, cache)
		}
		res, err := cpython.RunDriver(ctx, python, t.driver, nil, args...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(t.path, []byte(res.Stdout), 0o644); err != nil {
			return err
		}
		fmt.Printf("%-48s %8d bytes\n", t.path, len(res.Stdout))
	}
	for _, m := range manifests {
		if err := m.build(ctx, python, cache); err != nil {
			return fmt.Errorf("%s: %w", m.path, err)
		}
	}
	return nil
}

func (m manifest) build(ctx context.Context, python, cache string) error {
	res, err := cpython.RunDriver(ctx, python, "extract.py", nil, cache, m.group)
	if err != nil {
		return err
	}
	if res.Stderr != "" {
		fmt.Fprint(os.Stderr, m.group, ": ", res.Stderr)
	}
	requests := []byte(res.Stdout)
	cases, err := decode[corpus.Case](requests)
	if err != nil {
		return err
	}
	res, err = cpython.RunDriver(ctx, python, m.driver, requests, cache)
	if err != nil {
		return err
	}
	results, err := decode[result]([]byte(res.Stdout))
	if err != nil {
		return err
	}
	if len(results) != len(cases) {
		return fmt.Errorf("driver returned %d results for %d cases", len(results), len(cases))
	}
	for i := range cases {
		if results[i].ID != cases[i].ID {
			return fmt.Errorf("result %d is %q, want %q", i, results[i].ID, cases[i].ID)
		}
		cases[i].Expect = results[i].Expect
	}
	if err := corpus.Write(m.path, cases); err != nil {
		return err
	}
	report(m.path, cases)
	return nil
}

func decode[T any](data []byte) ([]T, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var out []T
	for dec.More() {
		var v T
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func report(path string, cases []corpus.Case) {
	var accepted, rejected, compile, warned int
	for _, c := range cases {
		switch {
		case c.Expect.Error == nil:
			accepted++
		case c.Expect.Stage == "compile":
			compile++
		default:
			rejected++
		}
		if len(c.Expect.Warnings) > 0 {
			warned++
		}
	}
	size := int64(0)
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	fmt.Printf("%-48s %6d cases: %d ok, %d error, %d compile-stage error, %d with warnings, %d bytes\n",
		path, len(cases), accepted, rejected, compile, warned, size)
}
