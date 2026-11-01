package cpython_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython"
)

func TestLookup(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		t.Setenv(cpython.EnvVar, "sh")
		path, err := cpython.Lookup()
		require.NoError(t, err)
		require.Equal(t, "sh", filepath.Base(path))
	})
	t.Run("override missing", func(t *testing.T) {
		t.Setenv(cpython.EnvVar, "definitely-not-an-interpreter")
		_, err := cpython.Lookup()
		require.ErrorIs(t, err, cpython.ErrNotFound)
	})
	t.Run("path", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "python3.14")
		require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755))
		t.Setenv(cpython.EnvVar, "")
		t.Setenv("PATH", dir)
		path, err := cpython.Lookup()
		require.NoError(t, err)
		require.Equal(t, fake, path)
	})
	t.Run("none", func(t *testing.T) {
		t.Setenv(cpython.EnvVar, "")
		t.Setenv("PATH", t.TempDir())
		_, err := cpython.Lookup()
		require.ErrorIs(t, err, cpython.ErrNotFound)
	})
}

func TestRun(t *testing.T) {
	python, err := cpython.Lookup()
	if err != nil {
		t.Skip("python 3.14 unavailable: ", err)
	}
	res, err := cpython.Run(context.Background(), python, "", nil, "-c", "import sys; print('ok'); sys.exit(3)")
	require.NoError(t, err)
	require.Equal(t, cpython.Result{Stdout: "ok\n", ExitCode: 3}, res)

	res, err = cpython.Run(context.Background(), python, "", []byte("print(7)"), "-")
	require.NoError(t, err)
	require.Equal(t, "7\n", res.Stdout)

	_, err = cpython.Run(context.Background(), "/nonexistent/python", "", nil)
	require.Error(t, err)

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	res, err = cpython.Run(context.Background(), python, dir, nil, "-c", "import os; print(os.getcwd())")
	require.NoError(t, err)
	require.Equal(t, dir+"\n", res.Stdout)
}
