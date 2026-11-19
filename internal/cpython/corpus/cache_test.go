package corpus_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython"
	"github.com/siyul-park/spry/internal/cpython/corpus"
)

func TestDir(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(corpus.EnvCache, dir)
		got, ok := corpus.Dir()
		require.Equal(t, dir, got)
		require.True(t, ok)
	})
	t.Run("override missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "none")
		t.Setenv(corpus.EnvCache, dir)
		got, ok := corpus.Dir()
		require.Equal(t, dir, got)
		require.False(t, ok)
	})
	t.Run("module default", func(t *testing.T) {
		t.Setenv(corpus.EnvCache, "")
		got, _ := corpus.Dir()
		require.True(t, strings.HasSuffix(got, filepath.Join(".cache", "cpython", cpython.Version)))
		require.FileExists(t, filepath.Join(got, "..", "..", "..", "go.mod"))
	})
}

func TestCase_Source(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(corpus.EnvCache, dir)
	data := []byte("print(1)\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.py"), data, 0o644))
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	tests := []struct {
		name    string
		c       corpus.Case
		want    string
		wantErr error
	}{
		{"inline", corpus.Case{Src: "x"}, "x", nil},
		{"inline empty", corpus.Case{}, "", nil},
		{"file", corpus.Case{File: "a.py", SHA256: good}, string(data), nil},
		{"drift", corpus.Case{File: "a.py", SHA256: "00"}, "", corpus.ErrDrift},
		{"escape", corpus.Case{File: "../a.py", SHA256: good}, "", os.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.Source()
			if tt.wantErr == os.ErrInvalid {
				require.Error(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, string(got))
		})
	}

	t.Run("no cache", func(t *testing.T) {
		t.Setenv(corpus.EnvCache, filepath.Join(dir, "none"))
		_, err := corpus.Case{File: "a.py"}.Source()
		require.ErrorIs(t, err, corpus.ErrNoCache)
	})
}
