package types

import "fmt"

// Instantiate applies the generic class, alias or signature orig to targs.
// Missing trailing arguments take the PEP 696 defaults of their parameters; a
// TypeVarTuple takes the arguments in its position as one packed *Tuple and a
// ParamSpec takes a *Tuple of parameters or another ParamSpec. An arity,
// kind or bound violation is an error that wraps ErrTypeArg.
func Instantiate(orig Type, targs []Type) (Type, error) {
	var tparams *TypeParamList
	switch o := orig.(type) {
	case *Named:
		if o.targs != nil {
			return nil, fmt.Errorf("%w: %s is already instantiated", ErrTypeArg, o)
		}
		tparams = o.tparams
	case *Alias:
		if o.targs != nil {
			return nil, fmt.Errorf("%w: %s is already instantiated", ErrTypeArg, o)
		}
		tparams = o.tparams
	case *Signature:
		tparams = o.tparams
	}
	if tparams.Len() == 0 {
		return nil, fmt.Errorf("%w: %s is not generic", ErrTypeArg, typeName(orig))
	}
	args, err := bindArgs(tparams, targs)
	if err != nil {
		return nil, err
	}
	switch o := orig.(type) {
	case *Named:
		return newInstance(o, args), nil
	case *Alias:
		return newAliasInstance(o, args), nil
	}
	sig := orig.(*Signature)
	m := make(substMap, len(args))
	for i, tp := range tparams.slice() {
		m[tp] = args[i]
	}
	return subst(&Signature{params: sig.params, result: sig.result}, m), nil
}

func bindArgs(tparams *TypeParamList, targs []Type) ([]Type, error) {
	n := tparams.Len()
	targs = packParamSpec(tparams, targs)
	args, err := arrange(tparams, targs)
	if err != nil {
		return nil, err
	}
	m := make(substMap, n)
	for i, tp := range tparams.slice() {
		if err := checkArg(tp, args[i], m); err != nil {
			return nil, err
		}
		m[tp] = args[i]
	}
	return args, nil
}

func packParamSpec(tparams *TypeParamList, targs []Type) []Type {
	if tparams.Len() != 1 || tparams.At(0).kind != ParamSpec || len(targs) == 0 {
		return targs
	}
	if len(targs) == 1 && isParamSpecArg(targs[0]) {
		return targs
	}
	params := make([]*Var, len(targs))
	for i, t := range targs {
		params[i] = NewParam(0, "", t, PositionalOnly)
	}
	return []Type{NewTuple(params)}
}

func arrange(tparams *TypeParamList, targs []Type) ([]Type, error) {
	n := tparams.Len()
	for k, tp := range tparams.slice() {
		if tp.kind != TypeVarTuple {
			continue
		}
		if len(targs) < n-1 {
			return nil, fmt.Errorf("%w: too few type arguments: expected at least %d, got %d", ErrTypeArg, n-1, len(targs))
		}
		extra := len(targs) - (n - 1)
		args := append([]Type{}, targs[:k]...)
		args = append(args, elemTuple(targs[k:k+extra], -1))
		return append(args, targs[k+extra:]...), nil
	}
	if len(targs) > n {
		return nil, fmt.Errorf("%w: too many type arguments: expected at most %d, got %d", ErrTypeArg, n, len(targs))
	}
	args := append([]Type{}, targs...)
	m := make(substMap, n)
	for i, tp := range tparams.slice() {
		if i >= len(args) {
			if tp.def == nil {
				return nil, fmt.Errorf("%w: too few type arguments: no value for %s", ErrTypeArg, tp.obj.name)
			}
			args = append(args, subst(tp.def, m))
		}
		m[tp] = args[i]
	}
	return args, nil
}

func checkArg(tp *TypeParam, arg Type, m substMap) error {
	switch tp.kind {
	case ParamSpec:
		if !isParamSpecArg(arg) {
			return fmt.Errorf("%w: %s is not a parameter list for %s", ErrTypeArg, arg, tp.obj.name)
		}
		return nil
	case TypeVarTuple:
		if _, ok := arg.(*Tuple); !ok && !isVarTuple(arg) {
			return fmt.Errorf("%w: %s is not a type list for %s", ErrTypeArg, arg, tp.obj.name)
		}
		return nil
	}
	if tp.bound != nil && !AssignableTo(arg, subst(tp.bound, m)) {
		return fmt.Errorf("%w: %s is not assignable to the bound %s of %s", ErrTypeArg, arg, tp.bound, tp.obj.name)
	}
	if len(tp.constraints) > 0 && !anyAssignable(arg, tp.constraints) {
		return fmt.Errorf("%w: %s violates the constraints of %s", ErrTypeArg, arg, tp.obj.name)
	}
	return nil
}

func isParamSpecArg(t Type) bool {
	if tp, ok := t.(*TypeParam); ok {
		return tp.kind == ParamSpec
	}
	_, ok := t.(*Tuple)
	return ok
}

func anyAssignable(arg Type, targets []Type) bool {
	for _, t := range targets {
		if AssignableTo(arg, t) {
			return true
		}
	}
	return false
}
