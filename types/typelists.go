package types

// TypeList is an immutable list of types, such as the type arguments of a
// Named. A nil *TypeList is empty.
type TypeList struct {
	types []Type
}

// TypeParamList is an immutable list of type parameters. A nil
// *TypeParamList is empty.
type TypeParamList struct {
	tparams []*TypeParam
}

// Len returns the number of types in l.
func (l *TypeList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.types)
}

// At returns the i-th type of l.
func (l *TypeList) At(i int) Type {
	return l.types[i]
}

// Len returns the number of type parameters in l.
func (l *TypeParamList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.tparams)
}

// At returns the i-th type parameter of l.
func (l *TypeParamList) At(i int) *TypeParam {
	return l.tparams[i]
}

func newTypeList(types []Type) *TypeList {
	if len(types) == 0 {
		return nil
	}
	return &TypeList{types: types}
}

func newTypeParamList(tparams []*TypeParam) *TypeParamList {
	if len(tparams) == 0 {
		return nil
	}
	return &TypeParamList{tparams: tparams}
}

func (l *TypeList) slice() []Type {
	if l == nil {
		return nil
	}
	return l.types
}

func (l *TypeParamList) slice() []*TypeParam {
	if l == nil {
		return nil
	}
	return l.tparams
}
