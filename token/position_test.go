package token_test

import (
	"testing"

	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

func TestNewFileSet(t *testing.T) {
	fset := token.NewFileSet()
	require.Equal(t, 1, fset.Base())
	require.Equal(t, token.Position{}, fset.Position(1))
}

func TestPos_IsValid(t *testing.T) {
	require.False(t, token.NoPos.IsValid())
	require.True(t, token.Pos(1).IsValid())
}

func TestPosition_IsValid(t *testing.T) {
	require.False(t, token.Position{}.IsValid())
	require.False(t, token.Position{Filename: "a.py"}.IsValid())
	require.True(t, token.Position{Line: 1}.IsValid())
}

func TestFileSet_Base(t *testing.T) {
	fset := token.NewFileSet()
	fset.AddFile("a.py", -1, 10)
	require.Equal(t, 12, fset.Base())
}

func TestFileSet_AddFile(t *testing.T) {
	fset := token.NewFileSet()
	a := fset.AddFile("a.py", -1, 10)
	b := fset.AddFile("b.py", -1, 0)
	c := fset.AddFile("c.py", 50, 3)
	require.Equal(t, 1, a.Base())
	require.Equal(t, 12, b.Base())
	require.Equal(t, 50, c.Base())
	require.Equal(t, 54, fset.Base())
	require.Panics(t, func() { fset.AddFile("d.py", 10, 1) })
	require.Panics(t, func() { fset.AddFile("d.py", -1, -1) })
	require.Panics(t, func() { fset.AddFile("d.py", int(^uint(0)>>1), 1) })
}

func TestFileSet_File(t *testing.T) {
	fset := token.NewFileSet()
	a := fset.AddFile("a.py", -1, 10)
	b := fset.AddFile("b.py", 20, 5)
	require.Nil(t, fset.File(token.NoPos))
	require.Same(t, a, fset.File(a.Pos(0)))
	require.Same(t, a, fset.File(a.Pos(10)))
	require.Nil(t, fset.File(token.Pos(15)))
	require.Same(t, b, fset.File(b.Pos(3)))
	require.Nil(t, fset.File(token.Pos(26)))
}

func TestFileSet_Position(t *testing.T) {
	fset := token.NewFileSet()
	a := fset.AddFile("a.py", -1, 10)
	b := fset.AddFile("b.py", -1, 10)
	b.AddLine(5)
	require.Equal(t, token.Position{Filename: "a.py", Offset: 3, Line: 1, Column: 4}, fset.Position(a.Pos(3)))
	require.Equal(t, token.Position{Filename: "b.py", Offset: 6, Line: 2, Column: 2}, fset.Position(b.Pos(6)))
	require.Equal(t, token.Position{}, fset.Position(token.NoPos))
	require.Equal(t, token.Position{}, fset.Position(token.Pos(1000)))
}

func TestFile_Name(t *testing.T) {
	_, f := newFile(t)
	require.Equal(t, "a.py", f.Name())
}

func TestFile_Base(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", 10, 5)
	require.Equal(t, 10, f.Base())
}

func TestFile_Size(t *testing.T) {
	_, f := newFile(t)
	require.Equal(t, 12, f.Size())
}

func TestFile_LineCount(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", -1, 10)
	require.Equal(t, 1, f.LineCount())
	f.AddLine(5)
	require.Equal(t, 2, f.LineCount())
}

func TestFile_AddLine(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", -1, 10)
	f.AddLine(4)
	f.AddLine(4)  // not increasing
	f.AddLine(2)  // not increasing
	f.AddLine(10) // not less than size
	f.AddLine(7)
	require.Equal(t, 3, f.LineCount())
	require.Equal(t, token.Pos(f.Base()+7), f.LineStart(3))
}

func TestFile_SetLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []int
		want  bool
	}{
		{"valid", []int{0, 3, 9}, true},
		{"empty", nil, true},
		{"not starting at zero", []int{1, 3}, false},
		{"not increasing", []int{0, 3, 3}, false},
		{"decreasing", []int{0, 5, 3}, false},
		{"beyond size", []int{0, 12}, false},
		{"negative", []int{-1, 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := token.NewFileSet().AddFile("a.py", -1, 12)
			require.Equal(t, tt.want, f.SetLines(tt.lines))
			if !tt.want {
				require.Equal(t, 1, f.LineCount())
			}
		})
	}
}

func TestFile_LineStart(t *testing.T) {
	_, f := newFile(t)
	require.Equal(t, f.Pos(0), f.LineStart(1))
	require.Equal(t, f.Pos(8), f.LineStart(3))
	require.Panics(t, func() { f.LineStart(0) })
	require.Panics(t, func() { f.LineStart(4) })
}

func TestFile_Pos(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", 100, 5)
	require.Equal(t, token.Pos(100), f.Pos(0))
	require.Equal(t, token.Pos(105), f.Pos(5))
	require.Panics(t, func() { f.Pos(6) })
	require.Panics(t, func() { f.Pos(-1) })
}

func TestFile_Offset(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", 100, 5)
	require.Equal(t, 0, f.Offset(100))
	require.Equal(t, 5, f.Offset(105))
	require.Panics(t, func() { f.Offset(99) })
	require.Panics(t, func() { f.Offset(106) })
}

func TestFile_Line(t *testing.T) {
	_, f := newFile(t)
	require.Equal(t, 1, f.Line(f.Pos(3)))
	require.Equal(t, 2, f.Line(f.Pos(4)))
	require.Equal(t, 3, f.Line(f.Pos(12)))
}

func TestFile_Position(t *testing.T) {
	_, f := newFile(t)
	tests := []struct {
		name string
		pos  token.Pos
		want token.Position
	}{
		{"no pos", token.NoPos, token.Position{}},
		{"first byte", f.Pos(0), token.Position{Filename: "a.py", Offset: 0, Line: 1, Column: 1}},
		{"middle", f.Pos(6), token.Position{Filename: "a.py", Offset: 6, Line: 2, Column: 3}},
		{"end", f.Pos(12), token.Position{Filename: "a.py", Offset: 12, Line: 3, Column: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, f.Position(tt.pos))
		})
	}
	require.Panics(t, func() { f.Position(token.Pos(f.Base() + 13)) })
}

func TestPosition_String(t *testing.T) {
	tests := []struct {
		name string
		pos  token.Position
		want string
	}{
		{"zero", token.Position{}, "-"},
		{"file only", token.Position{Filename: "a.py"}, "a.py"},
		{"file line column", token.Position{Filename: "a.py", Line: 3, Column: 5}, "a.py:3:5"},
		{"file line", token.Position{Filename: "a.py", Line: 3}, "a.py:3"},
		{"line column", token.Position{Line: 3, Column: 5}, "3:5"},
		{"line", token.Position{Line: 3}, "3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.pos.String())
		})
	}
}

func newFile(t *testing.T) (*token.FileSet, *token.File) {
	t.Helper()
	fset := token.NewFileSet()
	f := fset.AddFile("a.py", -1, 12)
	require.True(t, f.SetLines([]int{0, 4, 8}))
	return fset, f
}
