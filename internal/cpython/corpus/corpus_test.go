package corpus_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython/corpus"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, "m.jsonl")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		return path
	}

	t.Run("round trip", func(t *testing.T) {
		want := []corpus.Case{
			{ID: "a", Mode: "exec", File: "Lib/x.py", SHA256: "00", Expect: corpus.Expect{Hash: "h"}},
			{ID: "b", Mode: "exec", Src: "é\x00"},
		}
		path := filepath.Join(dir, "rt.jsonl")
		require.NoError(t, corpus.Write(path, want))
		got, err := corpus.Load(path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("missing", func(t *testing.T) {
		_, err := corpus.Load(filepath.Join(dir, "none.jsonl"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
	t.Run("malformed", func(t *testing.T) {
		_, err := corpus.Load(write("{\"id\":\n"))
		require.Error(t, err)
	})
	t.Run("unknown field", func(t *testing.T) {
		_, err := corpus.Load(write(`{"id":"a","mode":"exec","bogus":1}` + "\n"))
		require.Error(t, err)
	})
	t.Run("duplicate", func(t *testing.T) {
		_, err := corpus.Load(write(`{"id":"a","mode":"x"}` + "\n" + `{"id":"a","mode":"x"}` + "\n"))
		require.ErrorIs(t, err, corpus.ErrDuplicate)
	})
}

func TestWrite(t *testing.T) {
	cases := []corpus.Case{
		{ID: "b", Mode: "exec", Src: "x = '<&>'", Expect: corpus.Expect{
			Error: &corpus.SyntaxError{Type: "SyntaxError", Msg: "m", Lineno: 1}, Stage: "parse", Warnings: []string{"w"}}},
		{ID: "a", Mode: "eval", Src: "1", Expect: corpus.Expect{Dump: "d"}},
	}
	path := filepath.Join(t.TempDir(), "sub", "m.jsonl")
	require.NoError(t, corpus.Write(path, cases))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"id":"a","mode":"eval","src":"1","expect":{"dump":"d"}}
{"id":"b","mode":"exec","src":"x = '<&>'","expect":{"error":{"type":"SyntaxError","msg":"m","lineno":1},"stage":"parse","warnings":["w"]}}
`, string(data))

	err = corpus.Write(path, []corpus.Case{{ID: "a"}, {ID: "a"}})
	require.ErrorIs(t, err, corpus.ErrDuplicate)
}
