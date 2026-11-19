package corpus_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython/corpus"
)

func TestFetch(t *testing.T) {
	data := archive(t, map[string]string{
		"cpython-x/Lib/a.py":        "A",
		"cpython-x/Lib/sub/b.py":    "B",
		"cpython-x/Grammar/g":       "G",
		"cpython-x/Parser/p":        "P",
		"cpython-x/Doc/skip.rst":    "no",
		"cpython-x/README.rst":      "no",
		"cpython-x/Lib/../../evil":  "no",
		"cpython-x/Libx/not-lib.py": "no",
	})
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	ctx := context.Background()

	t.Run("extracts and is idempotent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache", "3.14.9")
		require.NoError(t, corpus.Fetch(ctx, dir, corpus.WithArchive(srv.URL, hexSum)))
		for path, want := range map[string]string{"Lib/a.py": "A", "Lib/sub/b.py": "B", "Grammar/g": "G", "Parser/p": "P"} {
			got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
			require.NoError(t, err)
			require.Equal(t, want, string(got))
		}
		require.NoFileExists(t, filepath.Join(dir, "Doc", "skip.rst"))
		require.NoFileExists(t, filepath.Join(dir, "README.rst"))
		require.NoFileExists(t, filepath.Join(dir, "Libx", "not-lib.py"))
		require.NoFileExists(t, filepath.Join(filepath.Dir(dir), "evil"))

		before := hits.Load()
		require.NoError(t, corpus.Fetch(ctx, dir, corpus.WithArchive(srv.URL, hexSum)))
		require.Equal(t, before, hits.Load())

		entries, err := os.ReadDir(filepath.Dir(dir))
		require.NoError(t, err)
		require.Len(t, entries, 1)
	})
	t.Run("checksum mismatch", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		err := corpus.Fetch(ctx, dir, corpus.WithArchive(srv.URL, "00"))
		require.ErrorIs(t, err, corpus.ErrChecksum)
		require.NoDirExists(t, dir)
	})
	t.Run("http error", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		require.Error(t, corpus.Fetch(ctx, dir, corpus.WithArchive(srv.URL+"/missing", hexSum)))
		require.NoDirExists(t, dir)
	})
}

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}
