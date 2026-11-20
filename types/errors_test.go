package types_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

func TestError(t *testing.T) {
	at := token.Position{Filename: "a.py", Line: 3, Column: 5}

	t.Run("message", func(t *testing.T) {
		e := &types.Error{Pos: at, Code: "TYP110", Msg: "no such attribute"}
		require.Equal(t, "a.py:3:5: error TYP110: no such attribute", e.Error())
		require.Equal(t, "error TYP110: x", (&types.Error{Code: "TYP110", Msg: "x"}).Error())
	})
}

func TestError_Is(t *testing.T) {
	t.Run("categories", func(t *testing.T) {
		tests := []struct {
			code string
			is   error
		}{
			{"TYP112", types.ErrUnsupported},
			{"TYP150", types.ErrUnsupported},
			{"TYP140", types.ErrAnnotation},
		}
		for _, tt := range tests {
			require.ErrorIs(t, &types.Error{Code: tt.code}, tt.is)
		}
		require.False(t, errors.Is(&types.Error{Code: "TYP140"}, types.ErrUnsupported))
		require.False(t, errors.Is(&types.Error{Code: "TYP200"}, types.ErrAnnotation))
	})
}

func TestErrorList(t *testing.T) {
	var list types.ErrorList
	require.NoError(t, list.Err())
	require.Equal(t, "no errors", list.Error())

	list.Add(token.Position{Line: 2, Column: 1}, token.Position{}, "TYP150", "later")
	list.Add(token.Position{Line: 1, Column: 1}, token.Position{}, "TYP140", "earlier")
	require.Equal(t, 2, list.Len())
	require.Contains(t, list.Error(), "and 1 more")
	list.Sort()
	require.Equal(t, "earlier", list[0].Msg)
	require.ErrorIs(t, list.Err(), types.ErrAnnotation)
	require.ErrorIs(t, list.Err(), types.ErrUnsupported)

	one := types.ErrorList{list[0]}
	require.Equal(t, list[0].Error(), one.Error())
}
