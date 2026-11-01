// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gomodel describes the JSON shape of connector Go models without a target language.
package gomodel

import (
	"fmt"
	"go/constant"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Kind identifies a JSON-compatible Go value category.
type Kind string

const (
	// String is a JSON string.
	String Kind = "string"
	// Bool is a JSON boolean.
	Bool Kind = "bool"
	// Int is an integer represented safely by ordinary JavaScript numbers.
	Int Kind = "int"
	// Int64 is a Go 64-bit integer; consumers must account for JavaScript's 2^53 precision limit.
	Int64 Kind = "int64"
	// Float is a JSON number.
	Float Kind = "float"
	// Time is a time.Time JSON timestamp.
	Time Kind = "time"
	// Bytes is a base64-encoded byte slice.
	Bytes Kind = "bytes"
	// Unknown is a JSON value without a more specific known shape.
	Unknown Kind = "unknown"
	// List is a JSON array.
	List Kind = "list"
	// Map is a JSON object with dynamic keys.
	Map Kind = "map"
	// Ref refers to a Model by name.
	Ref Kind = "ref"
	// Enum is a named string or integer type with constants in its declaring package.
	Enum Kind = "enum"
)

// Type describes a JSON value, including nullability within collections.
type Type struct {
	Kind       Kind
	Name       string
	Elem       *Type
	Key        *Type
	Ref        string
	EnumBase   Kind
	EnumValues []string
	Nullable   bool
}

// Field describes a JSON key; Optional means encoding/json can omit that key.
type Field struct {
	Name     string
	Type     Type
	Optional bool
}

// Model describes a named or synthesized JSON object.
type Model struct {
	Name   string
	Fields []Field
}

// Graph holds all models reachable from the requested root types.
type Graph struct {
	Models []Model
}

type loader struct {
	packageTypes *types.Package
	models       []Model
	seen         map[string]bool
	active       map[string]bool
	reserved     map[string]bool
}

type embeddedStruct struct {
	structure *types.Struct
	index     []int
	optional  bool
	count     int
}

type fieldCandidate struct {
	name     string
	field    *types.Var
	index    []int
	tagged   bool
	optional bool
}

// Load resolves root Go struct names and their reachable JSON models from a connector module.
func Load(goDirectory string, rootNames []string) (Graph, error) {
	loaded, err := packages.Load(&packages.Config{Dir: goDirectory, Mode: packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles}, ".")
	if err != nil {
		return Graph{}, fmt.Errorf("load connector Go package: %w", err)
	}
	if len(loaded) != 1 {
		return Graph{}, fmt.Errorf("load connector Go package: expected one package, got %d", len(loaded))
	}
	for _, problem := range loaded[0].Errors {
		return Graph{}, fmt.Errorf("load connector Go package: %s", problem)
	}
	modelLoader := &loader{packageTypes: loaded[0].Types, seen: map[string]bool{}, active: map[string]bool{}, reserved: map[string]bool{}}
	for _, rootName := range rootNames {
		object := modelLoader.packageTypes.Scope().Lookup(rootName)
		if object == nil {
			return Graph{}, fmt.Errorf("Go model %s is missing from %s", rootName, goDirectory)
		}
		if err := modelLoader.addModel(rootName, object.Type()); err != nil {
			return Graph{}, err
		}
	}
	return Graph{Models: modelLoader.models}, nil
}

func (modelLoader *loader) addModel(name string, modelType types.Type) error {
	if modelLoader.seen[name] || modelLoader.active[name] {
		return nil
	}
	structure, ok := types.Unalias(modelType).Underlying().(*types.Struct)
	if !ok {
		return fmt.Errorf("Go model %s is not a struct", name)
	}
	modelLoader.active[name] = true
	candidates, err := modelLoader.jsonFields(structure)
	if err != nil {
		return fmt.Errorf("Go model %s: %w", name, err)
	}
	model := Model{Name: name, Fields: make([]Field, 0, len(candidates))}
	for _, candidate := range candidates {
		fieldType, err := modelLoader.describeType(candidate.field.Type(), name+candidate.field.Name())
		if err != nil {
			return fmt.Errorf("Go model %s.%s: %w", name, candidate.field.Name(), err)
		}
		tag := reflect.StructTag(candidateTag(structure, candidate.index)).Get("json")
		_, options := parseJSONTag(tag)
		optional := candidate.optional || slices.Contains(options, "omitempty") || slices.Contains(options, "omitzero")
		if optional && isNilable(candidate.field.Type()) {
			fieldType.Nullable = false
		}
		model.Fields = append(model.Fields, Field{Name: candidate.name, Type: fieldType, Optional: optional})
	}
	delete(modelLoader.active, name)
	modelLoader.seen[name] = true
	modelLoader.models = append(modelLoader.models, model)
	return nil
}

func (modelLoader *loader) jsonFields(structure *types.Struct) ([]fieldCandidate, error) {
	frontier := []embeddedStruct{{structure: structure, count: 1}}
	visited := map[*types.Struct]bool{}
	var candidates []fieldCandidate
	for len(frontier) > 0 {
		next := map[*types.Struct]*embeddedStruct{}
		order := []*types.Struct{}
		for _, embedded := range frontier {
			if visited[embedded.structure] {
				continue
			}
			visited[embedded.structure] = true
			for index := range embedded.structure.NumFields() {
				field := embedded.structure.Field(index)
				fieldType := types.Unalias(field.Type())
				baseType := fieldType
				if pointer, ok := baseType.(*types.Pointer); ok {
					baseType = types.Unalias(pointer.Elem())
				}
				_, embeddedIsStruct := baseType.Underlying().(*types.Struct)
				if !field.Exported() && (!field.Embedded() || !embeddedIsStruct) {
					continue
				}
				tag := reflect.StructTag(embedded.structure.Tag(index)).Get("json")
				if tag == "-" {
					continue
				}
				jsonName, options := parseJSONTag(tag)
				for _, option := range options {
					if option != "omitempty" && option != "omitzero" {
						return nil, fmt.Errorf("%s: unsupported json tag option %q", field.Name(), option)
					}
				}
				path := append(slices.Clone(embedded.index), index)
				if field.Embedded() && jsonName == "" && embeddedIsStruct {
					child := baseType.Underlying().(*types.Struct)
					if next[child] == nil {
						_, pointerEmbedded := fieldType.(*types.Pointer)
						next[child] = &embeddedStruct{structure: child, index: path, optional: embedded.optional || pointerEmbedded, count: embedded.count}
						order = append(order, child)
					} else {
						next[child].count += embedded.count
					}
					continue
				}
				if jsonName == "" {
					jsonName = field.Name()
				}
				candidate := fieldCandidate{name: jsonName, field: field, index: path, tagged: tagNamePresent(tag), optional: embedded.optional}
				candidates = append(candidates, candidate)
				if embedded.count > 1 {
					candidates = append(candidates, candidate)
				}
			}
		}
		frontier = frontier[:0]
		for _, child := range order {
			frontier = append(frontier, *next[child])
		}
	}
	slices.SortFunc(candidates, func(left, right fieldCandidate) int {
		if comparison := strings.Compare(left.name, right.name); comparison != 0 {
			return comparison
		}
		if len(left.index) != len(right.index) {
			return len(left.index) - len(right.index)
		}
		if left.tagged != right.tagged {
			if left.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(left.index, right.index)
	})
	selected := make([]fieldCandidate, 0, len(candidates))
	for index := 0; index < len(candidates); {
		end := index + 1
		for end < len(candidates) && candidates[end].name == candidates[index].name {
			end++
		}
		if end == index+1 || len(candidates[index].index) != len(candidates[index+1].index) || candidates[index].tagged != candidates[index+1].tagged {
			selected = append(selected, candidates[index])
		}
		index = end
	}
	slices.SortFunc(selected, func(left, right fieldCandidate) int { return slices.Compare(left.index, right.index) })
	return selected, nil
}

func (modelLoader *loader) describeType(modelType types.Type, syntheticName string) (Type, error) {
	modelType = types.Unalias(modelType)
	if pointer, ok := modelType.(*types.Pointer); ok {
		value, err := modelLoader.describeType(pointer.Elem(), syntheticName)
		value.Nullable = true
		return value, err
	}
	if named, ok := modelType.(*types.Named); ok {
		if named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time" {
			return Type{Kind: Time}, nil
		}
		if named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "encoding/json" && named.Obj().Name() == "RawMessage" {
			return Type{Kind: Unknown, Nullable: true}, nil
		}
		if isKnownCellValue(named) {
			return Type{Kind: Unknown, Nullable: true}, nil
		}
		if hasMarshalMethod(named, "MarshalJSON") || hasMarshalMethod(named, "MarshalText") {
			return Type{}, fmt.Errorf("%s implements a custom JSON or text marshaler without a known mapping", named.String())
		}
		if named.Obj().Pkg() == modelLoader.packageTypes {
			if _, ok := named.Underlying().(*types.Struct); ok {
				name := named.Obj().Name()
				if err := modelLoader.addModel(name, named); err != nil {
					return Type{}, err
				}
				return Type{Kind: Ref, Ref: name}, nil
			}
			if values := enumValues(named); len(values) > 0 {
				base, err := modelLoader.describeType(named.Underlying(), syntheticName)
				if err != nil {
					return Type{}, err
				}
				return Type{Kind: Enum, Name: named.Obj().Name(), EnumBase: base.Kind, EnumValues: values}, nil
			}
		}
		plain, err := modelLoader.describeType(named.Underlying(), syntheticName)
		plain.Name = named.Obj().Name()
		return plain, err
	}
	switch typed := modelType.(type) {
	case *types.Basic:
		switch {
		case typed.Info()&types.IsString != 0:
			return Type{Kind: String}, nil
		case typed.Info()&types.IsBoolean != 0:
			return Type{Kind: Bool}, nil
		case typed.Kind() == types.Int64 || typed.Kind() == types.Uint64:
			return Type{Kind: Int64}, nil
		case typed.Info()&types.IsInteger != 0:
			return Type{Kind: Int}, nil
		case typed.Info()&types.IsFloat != 0:
			return Type{Kind: Float}, nil
		}
	case *types.Slice:
		if basic, ok := types.Unalias(typed.Elem()).(*types.Basic); ok && basic.Kind() == types.Byte {
			return Type{Kind: Bytes, Nullable: true}, nil
		}
		element, err := modelLoader.describeType(typed.Elem(), syntheticName+"Item")
		return Type{Kind: List, Elem: &element, Nullable: true}, err
	case *types.Array:
		element, err := modelLoader.describeType(typed.Elem(), syntheticName+"Item")
		return Type{Kind: List, Elem: &element}, err
	case *types.Map:
		key, err := modelLoader.describeType(typed.Key(), syntheticName+"Key")
		if err != nil {
			return Type{}, err
		}
		if key.Kind != String && key.Kind != Int && key.Kind != Int64 {
			return Type{}, fmt.Errorf("unsupported map key %s", typed.Key())
		}
		element, err := modelLoader.describeType(typed.Elem(), syntheticName+"Value")
		return Type{Kind: Map, Key: &key, Elem: &element, Nullable: true}, err
	case *types.Interface:
		return Type{Kind: Unknown, Nullable: true}, nil
	case *types.Struct:
		name := syntheticName
		for modelLoader.reserved[name] || modelLoader.packageTypes.Scope().Lookup(name) != nil {
			name += "Inline"
		}
		modelLoader.reserved[name] = true
		if err := modelLoader.addModel(name, typed); err != nil {
			return Type{}, err
		}
		return Type{Kind: Ref, Ref: name}, nil
	}
	return Type{}, fmt.Errorf("unsupported Go type %s", modelType.String())
}

func enumValues(named *types.Named) []string {
	basic, ok := named.Underlying().(*types.Basic)
	if !ok || basic.Info()&(types.IsString|types.IsInteger) == 0 {
		return nil
	}
	var values []string
	scope := named.Obj().Pkg().Scope()
	for _, name := range scope.Names() {
		constantObject, ok := scope.Lookup(name).(*types.Const)
		if !ok || !types.Identical(types.Unalias(constantObject.Type()), named) {
			continue
		}
		if basic.Info()&types.IsString != 0 {
			values = append(values, constant.StringVal(constantObject.Val()))
		} else {
			values = append(values, constantObject.Val().ExactString())
		}
	}
	slices.Sort(values)
	return slices.Compact(values)
}

func hasMarshalMethod(modelType types.Type, methodName string) bool {
	for _, receiver := range []types.Type{modelType, types.NewPointer(modelType)} {
		method := types.NewMethodSet(receiver).Lookup(nil, methodName)
		if method == nil {
			continue
		}
		signature, ok := method.Obj().Type().(*types.Signature)
		if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 2 {
			continue
		}
		result, ok := signature.Results().At(0).Type().(*types.Slice)
		if !ok {
			continue
		}
		byteType, ok := result.Elem().(*types.Basic)
		if !ok || byteType.Kind() != types.Byte {
			continue
		}
		if signature.Results().At(1).Type().String() == "error" {
			return true
		}
	}
	return false
}

func isKnownCellValue(named *types.Named) bool {
	if named.Obj().Name() != "CellValue" || named.Obj().Pkg() == nil {
		return false
	}
	switch named.Obj().Pkg().Path() {
	case "github.com/superdurable/dex-connectors-library/connectors/airtable/go",
		"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/go":
		return true
	}
	return false
}

func candidateTag(structure *types.Struct, index []int) string {
	for _, position := range index[:len(index)-1] {
		modelType := types.Unalias(structure.Field(position).Type())
		if pointer, ok := modelType.(*types.Pointer); ok {
			modelType = types.Unalias(pointer.Elem())
		}
		structure = modelType.Underlying().(*types.Struct)
	}
	return structure.Tag(index[len(index)-1])
}

func parseJSONTag(tag string) (string, []string) {
	parts := strings.Split(tag, ",")
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], parts[1:]
}

func tagNamePresent(tag string) bool {
	name, _ := parseJSONTag(tag)
	return name != ""
}

func isNilable(modelType types.Type) bool {
	modelType = types.Unalias(modelType)
	if named, ok := modelType.(*types.Named); ok {
		modelType = named.Underlying()
	}
	switch modelType.(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface:
		return true
	}
	return false
}

// EnumLiteral returns the JSON literal for an enum value.
func EnumLiteral(value string, kind Kind) string {
	if kind == String {
		return strconv.Quote(value)
	}
	return value
}
