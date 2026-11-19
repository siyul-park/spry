package cpython_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr error
	}{
		{"pinned", cpython.Version, nil},
		{"other micro", "3.14.0", nil},
		{"other minor", "3.13.2", cpython.ErrVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := cpython.Check(context.Background(), fakePython(t, tt.version))
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.version, actual)
		})
	}

	_, err := cpython.Check(context.Background(), "/nonexistent/python")
	require.Error(t, err)
}

func fakePython(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "python")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho "+version+"\n"), 0o755))
	return path
}
