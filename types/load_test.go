package types_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/types"
)

func fsOf(files map[string]string) types.Option {
	m := fstest.MapFS{}
	for name, src := range files {
		m[name] = &fstest.MapFile{Data: []byte(src)}
	}
	return types.WithFS(m, "/app")
}

func importSites(c *checked) []ast.Node {
	var out []ast.Node
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Import:
			for _, a := range n.Names {
				out = append(out, a)
			}
		case *ast.ImportFrom:
			out = append(out, n)
		}
		return true
	})
	return out
}

func importedNames(m *types.Module) []string {
	var out []string
	for _, i := range m.Imports() {
		out = append(out, i.Name())
	}
	return out
}

func checkLoad(t *testing.T) {
	t.Run("module and package", func(t *testing.T) {
		c := mustCheck(t, "import lib\nimport pkg\nx = lib.value\n", fsOf(map[string]string{
			"lib.py":          "value = 1\n",
			"pkg/__init__.py": "name = 'pkg'\n",
		}))
		require.Equal(t, []string{"lib", "pkg"}, importedNames(c.mod))
		lib := c.mod.Imports()[0]
		for _, site := range importSites(c) {
			imp := importOf(t, c, site)
			require.Equal(t, types.ImportOK, imp.Kind)
			require.NoError(t, imp.Err)
		}
		require.Same(t, lib, importOf(t, c, importSites(c)[0]).Module)
		_, ok := c.info.Import(c.file)
		require.False(t, ok)
		require.Equal(t, "/app/lib.py", lib.Path())
		require.Equal(t, "/app/pkg/__init__.py", c.mod.Imports()[1].Path())
		mn, ok := c.mod.Scope().Lookup("lib").(*types.ModuleName)
		require.True(t, ok)
		require.Same(t, lib, mn.Imported())
		require.Equal(t, "<module 'lib'>", mn.Type().String())
		require.NotNil(t, lib.Scope().Lookup("value"))
	})

	t.Run("dotted import loads the parents first and binds the top package", func(t *testing.T) {
		c := mustCheck(t, "import a.b.c\n", fsOf(map[string]string{
			"a/__init__.py": "", "a/b/__init__.py": "", "a/b/c.py": "v = 1\n",
		}))
		require.Equal(t, []string{"a", "a.b", "a.b.c"}, importedNames(c.mod))
		mn := c.mod.Scope().Lookup("a").(*types.ModuleName)
		require.Equal(t, "a", mn.Imported().Name())
		alias := importSites(c)[0]
		require.Same(t, mn, c.info.Implicits[alias])
	})

	t.Run("import as binds the module itself", func(t *testing.T) {
		c := mustCheck(t, "import a.b as ab\n", fsOf(map[string]string{"a/__init__.py": "", "a/b.py": ""}))
		require.Equal(t, "a.b", c.mod.Scope().Lookup("ab").(*types.ModuleName).Imported().Name())
	})

	t.Run("from import shares the object", func(t *testing.T) {
		c := mustCheck(t, "from lib import f, C as D\n", fsOf(map[string]string{"lib.py": "def f(): pass\nclass C: pass\n"}))
		lib := c.mod.Imports()[0]
		require.Same(t, lib.Scope().Lookup("f"), c.mod.Scope().Lookup("f"))
		require.Same(t, lib.Scope().Lookup("C"), c.mod.Scope().Lookup("D"))
	})

	t.Run("relative import", func(t *testing.T) {
		c := mustCheck(t, "import pkg.mod\n", fsOf(map[string]string{
			"pkg/__init__.py": "", "pkg/mod.py": "from . import sibling\nfrom .sibling import v\nfrom .. import top\n", "pkg/sibling.py": "v = 1\n",
		}))
		mod := c.mod.Imports()[1]
		require.Equal(t, []string{"pkg", "pkg.sibling"}, importedNames(mod))
		require.NotNil(t, mod.Scope().Lookup("v"))
		require.IsType(t, &types.ModuleName{}, mod.Scope().Lookup("sibling"))
	})

	t.Run("relative import in the main module is recorded", func(t *testing.T) {
		c := mustCheck(t, "from . import x\n")
		require.Equal(t, types.ImportRelative, importOf(t, c, importSites(c)[0]).Kind)
	})

	t.Run("star import", func(t *testing.T) {
		c := mustCheck(t, "from lib import *\nfrom other import *\n", fsOf(map[string]string{
			"lib.py":   "__all__ = ['a']\na = 1\nb = 2\n",
			"other.py": "x = 1\n_y = 2\n",
		}))
		require.NotNil(t, c.mod.Scope().Lookup("a"))
		require.Nil(t, c.mod.Scope().Lookup("b"))
		require.NotNil(t, c.mod.Scope().Lookup("x"))
		require.Nil(t, c.mod.Scope().Lookup("_y"))
	})

	t.Run("star import needs a literal __all__", func(t *testing.T) {
		c := check(t, "from lib import *\n", fsOf(map[string]string{"lib.py": "__all__ = make()\n"}))
		var list types.ErrorList
		require.ErrorAs(t, c.err, &list)
		require.Equal(t, "TYP120", list[0].Code)
		require.ErrorIs(t, c.err, types.ErrUnsupported)
	})

	t.Run("cycle", func(t *testing.T) {
		c := mustCheck(t, "import a\n", fsOf(map[string]string{"a.py": "import b\nx = 1\n", "b.py": "import a\nfrom a import x\ny = 2\n"}))
		a := c.mod.Imports()[0]
		b := a.Imports()[0]
		require.Equal(t, "b", b.Name())
		require.Equal(t, []string{"a"}, importedNames(b))
		require.Same(t, a.Scope().Lookup("x"), b.Scope().Lookup("x"))
	})

	t.Run("local file shadows a shipped module", func(t *testing.T) {
		c := mustCheck(t, "import math\n", fsOf(map[string]string{"math.py": "tau = 1\n"}))
		require.Equal(t, "/app/math.py", c.mod.Imports()[0].Path())
		shipped := mustCheck(t, "import math\n")
		require.NotEqual(t, "/app/math.py", shipped.mod.Imports()[0].Path())
		require.NotNil(t, shipped.mod.Imports()[0].Scope().Lookup("sqrt"))
	})

	t.Run("shipped modules", func(t *testing.T) {
		c := mustCheck(t, "import sys\nimport typing\nimport collections.abc\nimport __future__\nfrom sys import argv\n")
		require.Equal(t, []string{"sys", "typing", "collections", "collections.abc", "__future__"}, importedNames(c.mod))
		require.Equal(t, "list[str]", c.mod.Scope().Lookup("argv").Type().String())
	})

	t.Run("deterministic order", func(t *testing.T) {
		src := "import b\nimport a\n"
		files := map[string]string{"a.py": "import c\n", "b.py": "import c\n", "c.py": ""}
		first := importedNames(mustCheck(t, src, fsOf(files)).mod)
		for range 5 {
			require.Equal(t, first, importedNames(mustCheck(t, src, fsOf(files)).mod))
		}
		require.Equal(t, []string{"b", "a"}, first)
	})

	t.Run("missing module is recorded", func(t *testing.T) {
		c := mustCheck(t, "import nothing_here\nfrom nothing_here import x\n")
		for _, site := range importSites(c) {
			imp := importOf(t, c, site)
			require.Equal(t, types.ImportNotFound, imp.Kind)
			require.Nil(t, imp.Module)
		}
		require.Empty(t, c.mod.Imports())
		require.IsType(t, &types.ModuleName{}, c.mod.Scope().Lookup("nothing_here"))
	})

	t.Run("syntax error in an imported module is recorded", func(t *testing.T) {
		c := mustCheck(t, "import broken\nimport worse\n", fsOf(map[string]string{"broken.py": "def (:\n", "worse.py": "return 1\n"}))
		sites := importSites(c)
		imp := importOf(t, c, sites[0])
		require.Equal(t, types.ImportSyntax, imp.Kind)
		require.ErrorIs(t, imp.Err, scanner.ErrSyntax)
		imp = importOf(t, c, sites[1])
		require.Equal(t, types.ImportSyntax, imp.Kind)
		require.Equal(t, "'return' outside function", imp.Err.(scanner.ErrorList)[0].Msg)
		require.Empty(t, c.mod.Imports())
	})

	t.Run("nested imports are loaded", func(t *testing.T) {
		c := mustCheck(t, "def f():\n    import lib\nif True:\n    import other\n", fsOf(map[string]string{"lib.py": "", "other.py": ""}))
		require.Equal(t, []string{"lib", "other"}, importedNames(c.mod))
	})

	t.Run("unshipped standard library module", func(t *testing.T) {
		c := check(t, "import os\nfrom json import loads\n")
		var list types.ErrorList
		require.ErrorAs(t, c.err, &list)
		require.Len(t, list, 2)
		require.Equal(t, "TYP121", list[0].Code)
		require.Equal(t, 1, list[0].Pos.Line)
		require.ErrorIs(t, c.err, types.ErrUnsupported)
	})

	t.Run("guarded import of an unshipped module", func(t *testing.T) {
		for _, guard := range []string{"ImportError", "ModuleNotFoundError", "Exception", "BaseException", "(KeyError, ImportError)"} {
			c := mustCheck(t, "try:\n    import os\nexcept "+guard+":\n    os = None\n")
			require.Equal(t, types.ImportUnshipped, importOf(t, c, importSites(c)[0]).Kind, guard)
		}
		c := mustCheck(t, "try:\n    import os\nexcept:\n    pass\n")
		require.NotNil(t, c.mod)
		c = check(t, "try:\n    import os\nexcept KeyError:\n    pass\n")
		require.Error(t, c.err)
		c = check(t, "try:\n    pass\nexcept ImportError:\n    import os\n")
		require.Error(t, c.err)
	})

	t.Run("reflection modules", func(t *testing.T) {
		for module, code := range map[string]string{"importlib": "TYP121", "inspect": "TYP130", "gc": "TYP130"} {
			c := check(t, "try:\n    import "+module+"\nexcept ImportError:\n    pass\n")
			var list types.ErrorList
			require.ErrorAs(t, c.err, &list, module)
			require.Equal(t, code, list[0].Code)
		}
	})

	t.Run("a local file wins over an unshipped module", func(t *testing.T) {
		c := mustCheck(t, "import os\n", fsOf(map[string]string{"os.py": "x = 1\n"}))
		require.Equal(t, "/app/os.py", c.mod.Imports()[0].Path())
	})

	t.Run("search order", func(t *testing.T) {
		m1 := fstest.MapFS{"m.py": {Data: []byte("a = 1\n")}}
		m2 := fstest.MapFS{"m.py": {Data: []byte("b = 1\n")}}
		c := mustCheck(t, "import m\n", types.WithFS(m1, "/one"), types.WithFS(m2, "/two"))
		require.Equal(t, "/one/m.py", c.mod.Imports()[0].Path())
		pkgAndModule := fstest.MapFS{"x/__init__.py": {}, "x.py": {}}
		c = mustCheck(t, "import x\n", types.WithFS(pkgAndModule, "/p"))
		require.Equal(t, "/p/x/__init__.py", c.mod.Imports()[0].Path())
	})

	t.Run("a submodule of a plain module is not found", func(t *testing.T) {
		c := mustCheck(t, "import m.sub\n", fsOf(map[string]string{"m.py": "", "m/sub.py": ""}))
		require.Equal(t, types.ImportNotFound, importOf(t, c, importSites(c)[0]).Kind)
	})

	t.Run("compile error of the main module", func(t *testing.T) {
		c := check(t, "import lib\nreturn 1\n", fsOf(map[string]string{"lib.py": ""}))
		require.Equal(t, "'return' outside function", syntaxError(t, c.err).Msg)
	})
}

func importOf(t *testing.T, c *checked, site ast.Node) types.Import {
	t.Helper()
	imp, ok := c.info.Import(site)
	require.True(t, ok, "no import recorded for the site")
	return imp
}
