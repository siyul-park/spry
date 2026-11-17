package token

import (
	"fmt"
	"sort"
	"sync"
)

// Pos is a compact encoding of a source position within a FileSet. It is the
// FileSet base of a file plus a byte offset into it.
type Pos int

// Position is a decoded source position. Line and Column are 1-based, and
// Column counts bytes. CPython's col_offset is Column-1. SyntaxError offsets
// count code points and are converted elsewhere.
type Position struct {
	Filename string // filename, if any
	Offset   int    // byte offset, starting at 0
	Line     int    // line number, starting at 1
	Column   int    // column number in bytes, starting at 1
}

// FileSet is a set of Files with disjoint Pos ranges. A FileSet is safe for
// concurrent use.
type FileSet struct {
	mutex sync.Mutex
	base  int
	files []*File
}

// File is a handle for a source file in a FileSet. It records line start
// offsets so that a Pos costs one word. A File is safe for concurrent use.
type File struct {
	name string
	base int
	size int

	mutex sync.Mutex
	lines []int // offsets of line starts; lines[0] == 0
}

// NoPos is the zero Pos; it has no position information.
const NoPos Pos = 0

// NewFileSet returns an empty FileSet whose first file gets base 1.
func NewFileSet() *FileSet {
	return &FileSet{base: 1}
}

// IsValid reports whether p carries position information.
func (p Pos) IsValid() bool {
	return p != NoPos
}

// IsValid reports whether the position has a line number.
func (p Position) IsValid() bool {
	return p.Line > 0
}

// Base returns the base that the next added file will get.
func (s *FileSet) Base() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.base
}

// AddFile adds a file of size bytes to the set and returns it. A negative
// base selects Base. The file covers Pos values [base, base+size], and the
// set base advances past it. AddFile panics if base is below Base or size is
// negative.
func (s *FileSet) AddFile(filename string, base, size int) *File {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if base < 0 {
		base = s.base
	}
	if base < s.base {
		panic(fmt.Sprintf("token: invalid base %d (should be >= %d)", base, s.base))
	}
	if size < 0 {
		panic(fmt.Sprintf("token: invalid size %d (should be >= 0)", size))
	}
	f := &File{name: filename, base: base, size: size, lines: []int{0}}
	base += size + 1
	if base < 0 {
		panic("token: Pos offset overflow (too many source files?)")
	}
	s.base = base
	s.files = append(s.files, f)
	return f
}

// File returns the file containing p, or nil if there is none.
func (s *FileSet) File(p Pos) *File {
	if p == NoPos {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	i := sort.Search(len(s.files), func(i int) bool { return s.files[i].base > int(p) }) - 1
	if i < 0 {
		return nil
	}
	if f := s.files[i]; int(p) <= f.base+f.size {
		return f
	}
	return nil
}

// Position decodes p using its file. It returns the zero Position if p
// belongs to no file.
func (s *FileSet) Position(p Pos) Position {
	if f := s.File(p); f != nil {
		return f.Position(p)
	}
	return Position{}
}

// Name returns the file name given to AddFile.
func (f *File) Name() string {
	return f.name
}

// Base returns the base Pos of the file.
func (f *File) Base() int {
	return f.base
}

// Size returns the size of the file in bytes.
func (f *File) Size() int {
	return f.size
}

// LineCount returns the number of lines in the file.
func (f *File) LineCount() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.lines)
}

// AddLine records the start offset of a new line. It ignores an offset that
// is not greater than the last line start or that is not less than the file
// size.
func (f *File) AddLine(offset int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if i := len(f.lines); (i == 0 || f.lines[i-1] < offset) && offset < f.size {
		f.lines = append(f.lines, offset)
	}
}

// SetLines replaces the line start offsets. They must start at 0, be strictly
// increasing and be less than the file size. It reports whether they were
// accepted; on false the file is unchanged.
func (f *File) SetLines(lines []int) bool {
	for i, offset := range lines {
		if i > 0 && offset <= lines[i-1] || f.size <= offset || offset < 0 {
			return false
		}
	}
	if len(lines) > 0 && lines[0] != 0 {
		return false
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.lines = append([]int(nil), lines...)
	return true
}

// LineStart returns the Pos of the first byte of the 1-based line. It panics
// if line is out of range.
func (f *File) LineStart(line int) Pos {
	if line < 1 {
		panic(fmt.Sprintf("token: invalid line number %d (should be >= 1)", line))
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if line > len(f.lines) {
		panic(fmt.Sprintf("token: invalid line number %d (should be < %d)", line, len(f.lines)))
	}
	return Pos(f.base + f.lines[line-1])
}

// Pos returns the Pos of the byte at offset. It panics if offset is greater
// than the file size.
func (f *File) Pos(offset int) Pos {
	if offset < 0 || offset > f.size {
		panic(fmt.Sprintf("token: invalid file offset %d (should be in [0, %d])", offset, f.size))
	}
	return Pos(f.base + offset)
}

// Offset returns the byte offset of p. It panics if p is outside the file.
func (f *File) Offset(p Pos) int {
	if int(p) < f.base || int(p) > f.base+f.size {
		panic(fmt.Sprintf("token: invalid Pos value %d (should be in [%d, %d])", int(p), f.base, f.base+f.size))
	}
	return int(p) - f.base
}

// Line returns the 1-based line number of p. It panics if p is outside the
// file.
func (f *File) Line(p Pos) int {
	return f.Position(p).Line
}

// Position decodes p. It returns the zero Position for NoPos and panics if p
// is outside the file.
func (f *File) Position(p Pos) Position {
	if p == NoPos {
		return Position{}
	}
	offset := f.Offset(p)
	pos := Position{Filename: f.name, Offset: offset}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	if i := sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > offset }) - 1; i >= 0 {
		pos.Line = i + 1
		pos.Column = offset - f.lines[i] + 1
	}
	return pos
}

// String formats the position as "file:line:column", "file:line", "line:column",
// "line" or "-", depending on which parts are present.
func (p Position) String() string {
	s := p.Filename
	if p.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprint(p.Line)
		if p.Column != 0 {
			s += fmt.Sprintf(":%d", p.Column)
		}
	}
	if s == "" {
		s = "-"
	}
	return s
}
