package ssa_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/ssa"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

// built is a program that went through Check and Build.
type built struct {
	prog *ssa.Program
	info *types.Info
	mod  *types.Module
}

var update = flag.Bool("update", false, "rewrite the testdata/*.ssa goldens")

func TestBuild(t *testing.T) {
	files, err := filepath.Glob("testdata/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".py")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			b := buildSource(t, filepath.Base(file), string(src))
			got := printed(t, b.prog)
			golden := strings.TrimSuffix(file, ".py") + ".ssa"
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
			require.NoError(t, ssa.Verify(b.prog))

			q, err := ssa.Parse(strings.NewReader(printed(t, b.prog, ssa.WithPositions())))

			require.NoError(t, err)
			require.Equal(t, b.prog, q)
		})
	}

	fixtures, err := filepath.Glob("../benchmarks/fixtures/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)
	for _, file := range fixtures {
		name := strings.TrimSuffix(filepath.Base(file), ".py")
		t.Run("benchmark "+name+" builds without fallbacks", func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)

			b := buildSource(t, name+".py", string(src)+"\nprint(run())\n")

			require.NoError(t, ssa.Verify(b.prog))
			require.Empty(t, b.info.Fallbacks)
		})
	}

	t.Run("runs a module once and starts at the main module", func(t *testing.T) {
		b := buildSource(t, "m.py", "x = 1\n")

		require.Same(t, b.prog.Modules[0].Init, b.prog.Entry)
		require.Equal(t, "<module>", b.prog.Entry.Name)
	})

	t.Run("names the specializations of a function", func(t *testing.T) {
		b := buildSource(t, "m.py", "def f(x):\n    return x\n\nf(1)\nf('a')\n")

		var names []string
		for _, fn := range b.prog.Modules[0].Functions {
			names = append(names, fn.Name)
		}
		require.Equal(t, []string{"<module>", "f#0", "f#1"}, names)
	})

	t.Run("builds a program whose unbound local raises before later code", func(t *testing.T) {
		for _, src := range []string{
			"def r():\n    A=A\n    def A(a):\n        pass\nr",
			"def pair():\n    a=b\n(pair)",
			"lambda b: A % b\n",
			"class Q:\n    def __enter__():0\ndef f():\n    with Q:\n        0\nf\n",
			"class Q:\n    def m():0\nQ().m()\n",
			"def s():(i*i or i in ())\n",
			"raise () from ()\n",
			"class A:\n    pass\nraise A() from 0\n",
			"def outer(a):\n    @a\n    def f():\n        (0)\n(outer)",
		} {
			b := buildSource(t, "m.py", src)

			require.NoError(t, ssa.Verify(b.prog), "%q", src)
		}
	})

	t.Run("reads back an empty tuple type", func(t *testing.T) {
		b := buildSource(t, "m.py", "()\n")

		q, err := ssa.Parse(strings.NewReader(printed(t, b.prog, ssa.WithPositions())))

		require.NoError(t, err)
		require.Equal(t, b.prog, q)
	})
}

func printed(t *testing.T, p *ssa.Program, opts ...ssa.PrintOption) string {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, ssa.Print(&out, p, opts...))
	return out.String()
}

func buildSource(t *testing.T, name, src string) built {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, strings.NewReader(src))
	require.NoError(t, err)
	mod, info, err := types.Check(fset, name, file, types.WithFS(os.DirFS("testdata"), "/app"))
	require.NoError(t, err)
	prog, err := ssa.Build(fset, mod, info)
	require.NoError(t, err)
	return built{prog: prog, info: info, mod: mod}
}

// FuzzSSA feeds arbitrary source through the whole pipeline. A program that
// parses must not make Check panic; a program Check accepts must build (Build
// verifies its result), and the text form of the program must read back to the
// same program.
func FuzzSSA(f *testing.F) {
	seeds, err := filepath.Glob("testdata/*.py")
	require.NoError(f, err)
	require.NotEmpty(f, seeds)
	for _, seed := range seeds {
		src, err := os.ReadFile(seed)
		require.NoError(f, err)
		f.Add(src)
	}
	for _, seed := range []string{"", "x = (", "def f(:\n", "import nosuchmod\n", "nonlocal x\n", "x: int = 'a'\n"} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		fset := token.NewFileSet()
		mod, err := parser.ParseFile(fset, "f.py", bytes.NewReader(src))
		if err != nil {
			return
		}
		checked, info, err := types.Check(fset, "f.py", mod)
		if err != nil {
			return
		}
		p, err := ssa.Build(fset, checked, info)
		require.NoError(t, err, "Build failed on a program Check accepted: %q", src)
		require.NoError(t, ssa.Verify(p), "%q", src)

		var text bytes.Buffer
		require.NoError(t, ssa.Print(&text, p, ssa.WithPositions()), "%q", src)
		q, err := ssa.Parse(strings.NewReader(text.String()))
		require.NoError(t, err, "%q\n%s", src, text.String())
		var again bytes.Buffer
		require.NoError(t, ssa.Print(&again, q, ssa.WithPositions()), "%q", src)
		require.Equal(t, text.String(), again.String(), "%q", src)
		if !strings.Contains(text.String(), "nan") { // NaN != NaN under require.Equal
			require.Equal(t, p, q, "%q", src)
		}
	})
}
