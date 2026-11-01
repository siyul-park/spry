package benchmarks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixtures(t *testing.T) {
	all, err := Fixtures()
	require.NoError(t, err)
	require.Len(t, all, 25)
	for _, f := range all {
		require.Contains(t, f.Source, "def run(", f.Name)
		require.NotEmpty(t, strings.TrimSpace(f.Expected), f.Name)
		require.True(t, strings.HasSuffix(f.Driver(), "print(run())\n"), f.Name)
	}
}

func TestSelect(t *testing.T) {
	all := []Fixture{{Name: "recursive-fib-20"}, {Name: "recursive-fib-35"}, {Name: "sieve"}}
	tests := []struct {
		name     string
		patterns []string
		want     []string
		wantErr  bool
	}{
		{"all", nil, []string{"recursive-fib-20", "recursive-fib-35", "sieve"}, false},
		{"prefix", []string{"recursive-fib"}, []string{"recursive-fib-20", "recursive-fib-35"}, false},
		{"exact", []string{"sieve", "recursive-fib-20"}, []string{"sieve", "recursive-fib-20"}, false},
		{"partial word is not a prefix", []string{"recursive"}, []string{"recursive-fib-20", "recursive-fib-35"}, false},
		{"unknown", []string{"nope"}, nil, true},
		{"mid-word", []string{"sie"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(all, tt.patterns)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var names []string
			for _, f := range got {
				names = append(names, f.Name)
			}
			require.Equal(t, tt.want, names)
		})
	}
}
