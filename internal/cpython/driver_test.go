package cpython_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython"
)

func TestRunDriver(t *testing.T) {
	python, err := cpython.Lookup()
	if err != nil {
		t.Skip("python 3.14 unavailable: ", err)
	}
	ctx := context.Background()
	cache := t.TempDir()

	tests := []struct {
		name   string
		driver string
		stdin  string
		want   string
	}{
		{
			"parse ok", "parse.py", `{"id":"a","mode":"eval","src":"1+2"}`,
			`"dump": "Expression(`,
		},
		{
			"parse error", "parse.py", `{"id":"a","mode":"exec","src":"1 +"}`,
			`"stage": "parse"`,
		},
		{
			"parse compile stage", "parse.py", `{"id":"a","mode":"exec","src":"return 1"}`,
			`"stage": "compile"`,
		},
		{
			"tokens", "tokens.py", `{"id":"a","src":"x = 1\n"}`,
			`NAME       'x'           (1, 0) (1, 1)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cpython.RunDriver(ctx, python, tt.driver, []byte(tt.stdin+"\n"), cache)
			require.NoError(t, err)
			var line map[string]any
			require.NoError(t, json.Unmarshal([]byte(res.Stdout), &line))
			out, err := json.MarshalIndent(line, "", " ")
			require.NoError(t, err)
			require.Contains(t, string(out), tt.want)
		})
	}

	t.Run("tables", func(t *testing.T) {
		res, err := cpython.RunDriver(ctx, python, "tables.py", nil, "token")
		require.NoError(t, err)
		require.Contains(t, res.Stdout, `"kwlist"`)
	})
	t.Run("typerepr", func(t *testing.T) {
		res, err := cpython.RunDriver(ctx, python, "typerepr.py", nil)
		require.NoError(t, err)
		require.Contains(t, res.Stdout, `"repr": "collections.abc.Callable[[int], str]"`)
	})
	t.Run("failure", func(t *testing.T) {
		_, err := cpython.RunDriver(ctx, python, "tables.py", nil, "bogus")
		require.ErrorIs(t, err, cpython.ErrDriver)
	})
	t.Run("unknown", func(t *testing.T) {
		_, err := cpython.RunDriver(ctx, python, "nope.py", nil)
		require.ErrorIs(t, err, cpython.ErrDriver)
	})
}
