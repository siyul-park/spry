package scanner_test

import (
	"errors"
	"testing"

	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

func TestErrorList_Add(t *testing.T) {
	var list scanner.ErrorList
	pos := token.Position{Line: 1, Column: 2}
	end := token.Position{Line: 1, Column: 4}

	list.Add(pos, end, "bad", scanner.ErrSyntax)

	require.Len(t, list, 1)
	require.Equal(t, &scanner.Error{Pos: pos, End: end, Msg: "bad", Err: scanner.ErrSyntax}, list[0])
}

func TestErrorList_Len(t *testing.T) {
	list := scanner.ErrorList{{Msg: "a"}, {Msg: "b"}}

	require.Equal(t, 2, list.Len())
	require.Equal(t, 0, scanner.ErrorList(nil).Len())
}

func TestErrorList_Swap(t *testing.T) {
	list := scanner.ErrorList{{Msg: "a"}, {Msg: "b"}}

	list.Swap(0, 1)

	require.Equal(t, "b", list[0].Msg)
	require.Equal(t, "a", list[1].Msg)
}

func TestErrorList_Less(t *testing.T) {
	list := scanner.ErrorList{
		{Pos: token.Position{Line: 1, Column: 1}, Msg: "a"},
		{Pos: token.Position{Line: 1, Column: 2}, Msg: "a"},
	}

	require.True(t, list.Less(0, 1))
	require.False(t, list.Less(1, 0))
}

func TestErrorList_Sort(t *testing.T) {
	var list scanner.ErrorList
	at := func(file string, line, col int) token.Position {
		return token.Position{Filename: file, Line: line, Column: col}
	}
	list.Add(at("b.py", 1, 1), token.Position{}, "z", scanner.ErrSyntax)
	list.Add(at("a.py", 2, 1), token.Position{}, "z", scanner.ErrSyntax)
	list.Add(at("a.py", 1, 5), token.Position{}, "z", scanner.ErrSyntax)
	list.Add(at("a.py", 1, 2), token.Position{}, "b", scanner.ErrSyntax)
	list.Add(at("a.py", 1, 2), token.Position{}, "a", scanner.ErrSyntax)

	list.Sort()

	var got []string
	for _, e := range list {
		got = append(got, e.Pos.String()+" "+e.Msg)
	}
	require.Equal(t, []string{"a.py:1:2 a", "a.py:1:2 b", "a.py:1:5 z", "a.py:2:1 z", "b.py:1:1 z"}, got)
}

func TestErrorList_Err(t *testing.T) {
	t.Run("is nil when empty", func(t *testing.T) {
		var list scanner.ErrorList

		require.NoError(t, list.Err())
	})

	t.Run("is the list otherwise", func(t *testing.T) {
		list := scanner.ErrorList{{Msg: "a", Err: scanner.ErrTab}}

		require.Equal(t, list, list.Err())
		require.ErrorIs(t, list.Err(), scanner.ErrSyntax)
	})
}

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *scanner.Error
		want string
	}{
		{"with a position", &scanner.Error{Pos: token.Position{Filename: "a.py", Offset: 4, Line: 2, Column: 3}, Msg: "invalid decimal literal"}, "a.py:2:3: invalid decimal literal"},
		{"without a position", &scanner.Error{Msg: "unknown encoding: foo"}, "unknown encoding: foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	e := &scanner.Error{Msg: "m", Err: scanner.ErrTab}

	require.Same(t, scanner.ErrTab, e.Unwrap())
}

func TestError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"tab is a tab error", &scanner.Error{Err: scanner.ErrTab}, scanner.ErrTab, true},
		{"tab is an indentation error", &scanner.Error{Err: scanner.ErrTab}, scanner.ErrIndentation, true},
		{"tab is a syntax error", &scanner.Error{Err: scanner.ErrTab}, scanner.ErrSyntax, true},
		{"indentation is an indentation error", &scanner.Error{Err: scanner.ErrIndentation}, scanner.ErrIndentation, true},
		{"indentation is a syntax error", &scanner.Error{Err: scanner.ErrIndentation}, scanner.ErrSyntax, true},
		{"indentation is not a tab error", &scanner.Error{Err: scanner.ErrIndentation}, scanner.ErrTab, false},
		{"syntax is a syntax error", &scanner.Error{Err: scanner.ErrSyntax}, scanner.ErrSyntax, true},
		{"syntax is not an indentation error", &scanner.Error{Err: scanner.ErrSyntax}, scanner.ErrIndentation, false},
		{"unrelated target", &scanner.Error{Err: scanner.ErrSyntax}, errors.New("SyntaxError"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

func TestErrorList_Error(t *testing.T) {
	tests := []struct {
		name string
		list scanner.ErrorList
		want string
	}{
		{"empty", nil, "no errors"},
		{"one", scanner.ErrorList{{Msg: "a"}}, "a"},
		{"several", scanner.ErrorList{{Msg: "a"}, {Msg: "b"}, {Msg: "c"}}, "a (and 2 more errors)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.list.Error())
		})
	}
}

func TestErrorList_Unwrap(t *testing.T) {
	list := scanner.ErrorList{{Msg: "a", Err: scanner.ErrTab}, {Msg: "b", Err: scanner.ErrSyntax}}

	errs := list.Unwrap()

	require.Len(t, errs, 2)
	require.Same(t, list[0], errs[0])
	require.ErrorIs(t, list, scanner.ErrTab)
}
