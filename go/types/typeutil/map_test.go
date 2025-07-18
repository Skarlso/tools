// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package typeutil_test

// TODO(adonovan):
// - test use of explicit hasher across two maps.
// - test hashcodes are consistent with equals for a range of types
//   (e.g. all types generated by type-checking some body of real code).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
	"golang.org/x/tools/internal/testenv"
)

var (
	tStr      = types.Typ[types.String]             // string
	tPStr1    = types.NewPointer(tStr)              // *string
	tPStr2    = types.NewPointer(tStr)              // *string, again
	tInt      = types.Typ[types.Int]                // int
	tChanInt1 = types.NewChan(types.RecvOnly, tInt) // <-chan int
	tChanInt2 = types.NewChan(types.RecvOnly, tInt) // <-chan int, again
)

func checkEqualButNotIdentical(t *testing.T, x, y types.Type, comment string) {
	if !types.Identical(x, y) {
		t.Errorf("%s: not equal: %s, %s", comment, x, y)
	}
	if x == y {
		t.Errorf("%s: identical: %v, %v", comment, x, y)
	}
}

func TestAxioms(t *testing.T) {
	checkEqualButNotIdentical(t, tPStr1, tPStr2, "tPstr{1,2}")
	checkEqualButNotIdentical(t, tChanInt1, tChanInt2, "tChanInt{1,2}")
}

func TestMap(t *testing.T) {
	var tmap *typeutil.Map

	// All methods but Set are safe on (*T)(nil).
	tmap.Len()
	tmap.At(tPStr1)
	tmap.Delete(tPStr1)
	tmap.KeysString()
	_ = tmap.String()

	tmap = new(typeutil.Map)

	// Length of empty map.
	if l := tmap.Len(); l != 0 {
		t.Errorf("Len() on empty Map: got %d, want 0", l)
	}
	// At of missing key.
	if v := tmap.At(tPStr1); v != nil {
		t.Errorf("At() on empty Map: got %v, want nil", v)
	}
	// Deletion of missing key.
	if tmap.Delete(tPStr1) {
		t.Errorf("Delete() on empty Map: got true, want false")
	}
	// Set of new key.
	if prev := tmap.Set(tPStr1, "*string"); prev != nil {
		t.Errorf("Set() on empty Map returned non-nil previous value %s", prev)
	}

	// Now: {*string: "*string"}

	// Length of non-empty map.
	if l := tmap.Len(); l != 1 {
		t.Errorf("Len(): got %d, want 1", l)
	}
	// At via insertion key.
	if v := tmap.At(tPStr1); v != "*string" {
		t.Errorf("At(): got %q, want \"*string\"", v)
	}
	// At via equal key.
	if v := tmap.At(tPStr2); v != "*string" {
		t.Errorf("At(): got %q, want \"*string\"", v)
	}
	// Iteration over sole entry.
	tmap.Iterate(func(key types.Type, value any) {
		if key != tPStr1 {
			t.Errorf("Iterate: key: got %s, want %s", key, tPStr1)
		}
		if want := "*string"; value != want {
			t.Errorf("Iterate: value: got %s, want %s", value, want)
		}
	})

	// Setion with key equal to present one.
	if prev := tmap.Set(tPStr2, "*string again"); prev != "*string" {
		t.Errorf("Set() previous value: got %s, want \"*string\"", prev)
	}

	// Setion of another association.
	if prev := tmap.Set(tChanInt1, "<-chan int"); prev != nil {
		t.Errorf("Set() previous value: got %s, want nil", prev)
	}

	// Now: {*string: "*string again", <-chan int: "<-chan int"}

	want1 := "{*string: \"*string again\", <-chan int: \"<-chan int\"}"
	want2 := "{<-chan int: \"<-chan int\", *string: \"*string again\"}"
	if s := tmap.String(); s != want1 && s != want2 {
		t.Errorf("String(): got %s, want %s", s, want1)
	}

	want1 = "{*string, <-chan int}"
	want2 = "{<-chan int, *string}"
	if s := tmap.KeysString(); s != want1 && s != want2 {
		t.Errorf("KeysString(): got %s, want %s", s, want1)
	}

	// Keys().
	I := types.Identical
	switch k := tmap.Keys(); {
	case I(k[0], tChanInt1) && I(k[1], tPStr1): // ok
	case I(k[1], tChanInt1) && I(k[0], tPStr1): // ok
	default:
		t.Errorf("Keys(): got %v, want %s", k, want2)
	}

	if l := tmap.Len(); l != 2 {
		t.Errorf("Len(): got %d, want 1", l)
	}
	// At via original key.
	if v := tmap.At(tPStr1); v != "*string again" {
		t.Errorf("At(): got %q, want \"*string again\"", v)
	}
	hamming := 1
	tmap.Iterate(func(key types.Type, value any) {
		switch {
		case I(key, tChanInt1):
			hamming *= 2 // ok
		case I(key, tPStr1):
			hamming *= 3 // ok
		}
	})
	if hamming != 6 {
		t.Errorf("Iterate: hamming: got %d, want %d", hamming, 6)
	}

	if v := tmap.At(tChanInt2); v != "<-chan int" {
		t.Errorf("At(): got %q, want \"<-chan int\"", v)
	}
	// Deletion with key equal to present one.
	if !tmap.Delete(tChanInt2) {
		t.Errorf("Delete() of existing key: got false, want true")
	}

	// Now: {*string: "*string again"}

	if l := tmap.Len(); l != 1 {
		t.Errorf("Len(): got %d, want 1", l)
	}
	// Deletion again.
	if !tmap.Delete(tPStr2) {
		t.Errorf("Delete() of existing key: got false, want true")
	}

	// Now: {}

	if l := tmap.Len(); l != 0 {
		t.Errorf("Len(): got %d, want %d", l, 0)
	}
	if s := tmap.String(); s != "{}" {
		t.Errorf("Len(): got %q, want %q", s, "")
	}
}

func TestMapGenerics(t *testing.T) {
	const src = `
package p

// Basic defined types.
type T1 int
type T2 int

// Identical methods.
func (T1) M(int) {}
func (T2) M(int) {}

// A constraint interface.
type C interface {
	~int | string
}

type I interface {
}

// A generic type.
type G[P C] int

// Generic functions with identical signature.
func Fa1[P C](p P) {}
func Fa2[Q C](q Q) {}

// Fb1 and Fb2 are identical and should be mapped to the same entry, even if we
// map their arguments first.
func Fb1[P any](x *P) {
	var y *P // Map this first.
	_ = y
}
func Fb2[Q any](x *Q) {
}

// G1 and G2 are mutually recursive, and have identical methods.
type G1[P any] struct{
	Field *G2[P]
}
func (G1[P]) M(G1[P], G2[P]) {}
type G2[Q any] struct{
	Field *G1[Q]
}
func (G2[P]) M(G1[P], G2[P]) {}

// Method type expressions on different generic types are different.
var ME1 = G1[int].M
var ME2 = G2[int].M

// ME1Type should have identical type as ME1.
var ME1Type func(G1[int], G1[int], G2[int])

// Examples from issue #51314
type Constraint[T any] any
func Foo[T Constraint[T]]() {}
func Fn[T1 ~*T2, T2 ~*T1](t1 T1, t2 T2) {}

// Bar and Baz are identical to Foo.
func Bar[P Constraint[P]]() {}
func Baz[Q any]() {} // The underlying type of Constraint[P] is any.
// But Quux is not.
func Quux[Q interface{ quux() }]() {}


type Issue56048_I interface{ m() interface { Issue56048_I } }
var Issue56048 = Issue56048_I.m

type Issue56048_Ib interface{ m() chan []*interface { Issue56048_Ib } }
var Issue56048b = Issue56048_Ib.m

// Non-generic alias
type NonAlias int
type Alias1 = NonAlias
type Alias2 = NonAlias

// Generic alias (requires go1.23)
// type SetOfInt = map[int]bool
// type Set[T comparable] = map[K]bool
// type SetOfInt2 = Set[int]
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var conf types.Config
	pkg, err := conf.Check("", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Collect types.
	scope := pkg.Scope()
	var (
		T1      = scope.Lookup("T1").Type().(*types.Named)
		T2      = scope.Lookup("T2").Type().(*types.Named)
		T1M     = T1.Method(0).Type()
		T2M     = T2.Method(0).Type()
		G       = scope.Lookup("G").Type()
		GInt1   = instantiate(t, G, types.Typ[types.Int])
		GInt2   = instantiate(t, G, types.Typ[types.Int])
		GStr    = instantiate(t, G, types.Typ[types.String])
		C       = scope.Lookup("C").Type()
		CI      = C.Underlying().(*types.Interface)
		I       = scope.Lookup("I").Type()
		II      = I.Underlying().(*types.Interface)
		U       = CI.EmbeddedType(0).(*types.Union)
		Fa1     = scope.Lookup("Fa1").Type().(*types.Signature)
		Fa2     = scope.Lookup("Fa2").Type().(*types.Signature)
		Fa1P    = Fa1.TypeParams().At(0)
		Fa2Q    = Fa2.TypeParams().At(0)
		Fb1     = scope.Lookup("Fb1").Type().(*types.Signature)
		Fb1x    = Fb1.Params().At(0).Type()
		Fb1y    = scope.Lookup("Fb1").(*types.Func).Scope().Lookup("y").Type()
		Fb2     = scope.Lookup("Fb2").Type().(*types.Signature)
		Fb2x    = Fb2.Params().At(0).Type()
		G1      = scope.Lookup("G1").Type().(*types.Named)
		G1M     = G1.Method(0).Type()
		G1IntM1 = instantiate(t, G1, types.Typ[types.Int]).(*types.Named).Method(0).Type()
		G1IntM2 = instantiate(t, G1, types.Typ[types.Int]).(*types.Named).Method(0).Type()
		G1StrM  = instantiate(t, G1, types.Typ[types.String]).(*types.Named).Method(0).Type()
		G2      = scope.Lookup("G2").Type()
		// See below.
		// G2M     = G2.Method(0).Type()
		G2IntM  = instantiate(t, G2, types.Typ[types.Int]).(*types.Named).Method(0).Type()
		ME1     = scope.Lookup("ME1").Type()
		ME1Type = scope.Lookup("ME1Type").Type()
		ME2     = scope.Lookup("ME2").Type()

		Constraint  = scope.Lookup("Constraint").Type()
		Foo         = scope.Lookup("Foo").Type()
		Fn          = scope.Lookup("Fn").Type()
		Bar         = scope.Lookup("Foo").Type()
		Baz         = scope.Lookup("Foo").Type()
		Quux        = scope.Lookup("Quux").Type()
		Issue56048  = scope.Lookup("Issue56048").Type()
		Issue56048b = scope.Lookup("Issue56048b").Type()

		// In go1.23 these will be *types.Alias; for now they are all int.
		NonAlias = scope.Lookup("NonAlias").Type()
		Alias1   = scope.Lookup("Alias1").Type()
		Alias2   = scope.Lookup("Alias2").Type()

		// Requires go1.23.
		// SetOfInt    = scope.Lookup("SetOfInt").Type()
		// Set         = scope.Lookup("Set").Type().(*types.Alias)
		// SetOfInt2   = scope.Lookup("SetOfInt2").Type()
	)

	tmap := new(typeutil.Map)

	steps := []struct {
		typ      types.Type
		name     string
		newEntry bool
	}{
		{T1, "T1", true},
		{T2, "T2", true},
		{G, "G", true},
		{C, "C", true},
		{CI, "CI", true},
		{U, "U", true},
		{I, "I", true},
		{II, "II", true}, // should not be identical to CI

		// Methods can be identical, even with distinct receivers.
		{T1M, "T1M", true},
		{T2M, "T2M", false},

		// Identical instances should map to the same entry.
		{GInt1, "GInt1", true},
		{GInt2, "GInt2", false},
		// ..but instantiating with different arguments should yield a new entry.
		{GStr, "GStr", true},

		// F1 and F2 should have identical signatures.
		{Fa1, "F1", true},
		{Fa2, "F2", false},

		// The identity of P and Q should not have been affected by type parameter
		// masking during signature hashing.
		{Fa1P, "F1P", true},
		{Fa2Q, "F2Q", true},

		{Fb1y, "Fb1y", true},
		{Fb1x, "Fb1x", false},
		{Fb2x, "Fb2x", true},
		{Fb1, "Fb1", true},

		// Mapping elements of the function scope should not affect the identity of
		// Fb2 or Fb1.
		{Fb2, "Fb1", false},

		{G1, "G1", true},
		{G1M, "G1M", true},
		{G2, "G2", true},

		// See golang/go#49912: receiver type parameter names should be ignored
		// when comparing method identity.
		// {G2M, "G2M", false},
		{G1IntM1, "G1IntM1", true},
		{G1IntM2, "G1IntM2", false},
		{G1StrM, "G1StrM", true},
		{G2IntM, "G2IntM", false}, // identical to G1IntM1

		{ME1, "ME1", true},
		{ME1Type, "ME1Type", false},
		{ME2, "ME2", true},

		// See golang/go#51314: avoid infinite recursion on cyclic type constraints.
		{Constraint, "Constraint", true},
		{Foo, "Foo", true},
		{Fn, "Fn", true},
		{Bar, "Bar", false},
		{Baz, "Baz", false},
		{Quux, "Quux", true},

		{Issue56048, "Issue56048", true},   // (not actually about generics)
		{Issue56048b, "Issue56048b", true}, // (not actually about generics)

		// All three types are identical.
		{NonAlias, "NonAlias", true},
		{Alias1, "Alias1", false},
		{Alias2, "Alias2", false},

		// Generic aliases: requires go1.23.
		// {SetOfInt, "SetOfInt", true},
		// {Set, "Set", false},
		// {SetOfInt2, "SetOfInt2", false},
	}

	for _, step := range steps {
		existing := tmap.At(step.typ)
		if (existing == nil) != step.newEntry {
			t.Errorf("At(%s) = %v, want new entry: %t", step.name, existing, step.newEntry)
		}
		tmap.Set(step.typ, step.name)
	}
}

func instantiate(t *testing.T, origin types.Type, targs ...types.Type) types.Type {
	inst, err := types.Instantiate(nil, origin, targs, true)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// BenchmarkMap stores the type of every expression in the net/http
// package in a map.
func BenchmarkMap(b *testing.B) {
	testenv.NeedsGoPackages(b)

	// Load all dependencies of net/http.
	cfg := &packages.Config{Mode: packages.LoadAllSyntax}
	pkgs, err := packages.Load(cfg, "net/http")
	if err != nil {
		b.Fatal(err)
	}

	// Gather all unique types.Type pointers (>67K) annotating the syntax.
	allTypes := make(map[types.Type]bool)
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, tv := range pkg.TypesInfo.Types {
			allTypes[tv.Type] = true
		}
	})
	b.ResetTimer()

	for range b.N {
		// De-duplicate the logically identical types.
		var tmap typeutil.Map
		for t := range allTypes {
			tmap.Set(t, nil)
		}

		// For sanity, ensure we find a minimum number
		// of distinct type equivalence classes.
		if want := 12000; tmap.Len() < want {
			b.Errorf("too few types (from %d types.Type values, got %d logically distinct types, want >=%d)",
				len(allTypes),
				tmap.Len(),
				want)
		}
	}
}
