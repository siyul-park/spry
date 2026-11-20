package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestTypeParam(t *testing.T) {
	w := newWorld(t, `
N = TypeVar('N', contravariant=True)
class G2(Generic[N]): ...
class Bounded[B: Animal = Dog]: ...
class Constrained[C: (int, str)]: ...
class Free[F]: ...
def spec[**P](*args: P.args, **kwargs: P.kwargs) -> None: ...
`)
	param := func(name string) *types.TypeParam { return w.named(t, name).TypeParams().At(0) }

	t.Run("declared by TypeVar", func(t *testing.T) {
		g, p := w.named(t, "G2"), w.at(t, "N").(*types.TypeParam)
		require.Same(t, p, g.TypeParams().At(0))
		require.Equal(t, "N", p.Obj().Name())
		require.Equal(t, 0, p.Index())
		require.Equal(t, types.TypeVar, p.Kind())
		require.Equal(t, types.Contravariant, p.Variance())
		require.False(t, p.Implicit())
		require.Same(t, g, g.Origin())
	})
	t.Run("bound and default", func(t *testing.T) {
		p := param("Bounded")
		require.Same(t, w.animal, p.Bound())
		require.Same(t, w.dog, p.Default())
		require.Same(t, w.animal.Underlying(), p.Underlying())
	})
	t.Run("constraints", func(t *testing.T) {
		require.Equal(t, []types.Type{w.integer, w.str}, param("Constrained").Constraints())
	})
	t.Run("unbound parameter stands for object", func(t *testing.T) {
		p := param("Free")
		require.Nil(t, p.Bound())
		require.Nil(t, p.Default())
		require.Same(t, w.object.(*types.Named).Underlying(), p.Underlying())
	})
	t.Run("ParamSpec arguments", func(t *testing.T) {
		s := w.at(t, "spec").(*types.Signature)
		p := s.TypeParams().At(0)
		args, kwargs := s.Params().At(0).Type().(*types.ParamSpecArgs), s.Params().At(1).Type().(*types.ParamSpecArgs)
		require.Equal(t, types.ParamSpec, p.Kind())
		require.Same(t, p, kwargs.ParamSpec())
		require.False(t, args.Kwargs())
		require.True(t, kwargs.Kwargs())
		require.Same(t, kwargs, kwargs.Underlying())
		require.Equal(t, "P.kwargs", kwargs.String())
		require.Equal(t, "P.args", args.String())
	})
}
