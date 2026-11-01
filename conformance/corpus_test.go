package conformance_test

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/conformance"
)

const corpusDir = "testdata"

func TestParseDirectives(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		want    []string
		wantErr bool
	}{
		{"none", "print(1)\n", nil, false},
		{"one", "# spry-divergence: bigint not yet supported\nprint(1)\n", []string{"bigint not yet supported"}, false},
		{"indented and many", "  # spry-divergence: a\n# spry-divergence:b\n", []string{"a", "b"}, false},
		{"no colon", "# spry-divergence reason\n", nil, true},
		{"empty reason", "# spry-divergence:   \n", nil, true},
		{"prefix typo", "# spry-divergence-x: y\n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := conformance.ParseDirectives(tt.src)
			if tt.wantErr {
				require.ErrorIs(t, err, conformance.ErrDirective)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"b/two.py":       {Data: []byte("# spry-divergence: why\nprint(2)\n")},
		"b/two.expected": {Data: []byte("2\n")},
		"a/one.py":       {Data: []byte("print(1)\n")},
		"a/one.expected": {Data: []byte("1\n")},
		"a/notes.txt":    {Data: []byte("ignored")},
	}
	cases, err := conformance.Load(fsys)
	require.NoError(t, err)
	require.Len(t, cases, 2)
	require.Equal(t, "a/one", cases[0].ID())
	require.Equal(t, "1\n", cases[0].Expected)
	require.Equal(t, []string{"why"}, cases[1].Divergences)
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"missing expected", fstest.MapFS{"a/x.py": {Data: []byte("pass")}}},
		{"bad directive", fstest.MapFS{"a/x.py": {Data: []byte("# spry-divergence\n")}, "a/x.expected": {}}},
		{"too deep", fstest.MapFS{"a/b/x.py": {Data: []byte("pass")}, "a/b/x.expected": {}}},
		{"no category", fstest.MapFS{"x.py": {Data: []byte("pass")}, "x.expected": {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := conformance.Load(tt.fsys)
			require.Error(t, err)
		})
	}
}

// TestCorpusHygiene fails when a program lacks .expected, an .expected has no
// program, or a directive is malformed.
func TestCorpusHygiene(t *testing.T) {
	cases, err := conformance.Load(os.DirFS(corpusDir))
	require.NoError(t, err)
	require.NotEmpty(t, cases)

	known := map[string]bool{}
	for _, c := range cases {
		require.False(t, known[c.ID()], "duplicate %s", c.ID())
		known[c.ID()] = true
	}
	require.NoError(t, walkExpected(t, known))
}

func walkExpected(t *testing.T, known map[string]bool) error {
	t.Helper()
	cats, err := os.ReadDir(corpusDir)
	if err != nil {
		return err
	}
	for _, cat := range cats {
		files, err := os.ReadDir(corpusDir + "/" + cat.Name())
		if err != nil {
			return err
		}
		for _, f := range files {
			name := f.Name()
			if len(name) > len(conformance.ExpectedExt) && name[len(name)-len(conformance.ExpectedExt):] == conformance.ExpectedExt {
				id := cat.Name() + "/" + name[:len(name)-len(conformance.ExpectedExt)]
				require.True(t, known[id], "orphan %s", name)
			}
		}
	}
	return nil
}
