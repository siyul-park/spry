package types

import "github.com/siyul-park/spry/token"

// definedness decides which reads of module-level names in functions may see
// the name unbound. A name is bound where its module body binds it
// unconditionally before the function can run: at the end of the body, and
// before every call of the body that reaches a read of the name. Import
// cycles make every name of a module in the cycle possibly unbound.
func (c *solver) definedness(live map[*spec]bool) {
	end := map[*Module]map[Object]Definedness{}
	for _, s := range c.inits {
		end[s.fn.src.mod] = s.end
	}
	early := map[Object]bool{}
	for _, s := range c.inits {
		for i, targets := range s.callsAt {
			for _, t := range targets {
				for obj := range c.reads(t, map[*spec]bool{}, map[Object]bool{}) {
					if d, ok := s.stmts[i][obj]; !ok || d != Defined {
						early[obj] = true
					}
				}
			}
		}
	}
	for _, s := range c.all() {
		if !live[s] {
			continue
		}
		for n, obj := range s.gnames {
			d := c.readState(obj, end, early)
			if d > c.info.Defined[n] {
				c.info.Defined[n] = d
			}
		}
	}
}

func (c *solver) readState(obj Object, end map[*Module]map[Object]Definedness, early map[Object]bool) Definedness {
	m := obj.Module()
	if c.srcs[m] == nil || obj.Pos() == token.NoPos {
		return Defined
	}
	if d, ok := end[m][obj]; !ok || d != Defined || early[obj] || c.cyclic(m) {
		return MaybeDefined
	}
	return Defined
}

// reads collects the module-level objects that s and what it calls read.
func (c *solver) reads(s *spec, seen map[*spec]bool, out map[Object]bool) map[Object]bool {
	if seen[s] {
		return out
	}
	seen[s] = true
	for _, obj := range s.gnames {
		out[obj] = true
	}
	for _, call := range s.calls {
		if call.Target != nil {
			c.reads(c.specs[call.Target], seen, out)
		}
	}
	return out
}

// cyclic reports whether m imports itself through other modules.
func (c *solver) cyclic(m *Module) bool {
	seen := map[*Module]bool{}
	var walk func(x *Module) bool
	walk = func(x *Module) bool {
		for _, y := range x.imports {
			if y == m {
				return true
			}
			if !seen[y] {
				seen[y] = true
				if walk(y) {
					return true
				}
			}
		}
		return false
	}
	return walk(m)
}

// all lists every spec in creation order.
func (c *solver) all() []*spec {
	var out []*spec
	for _, s := range c.inits {
		out = append(out, s)
	}
	for _, f := range c.fns {
		if f.def != nil {
			out = append(out, f.list...)
		}
	}
	return out
}
