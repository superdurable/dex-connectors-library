// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// GoModel describes one connector struct shared with a language package.
type GoModel struct {
	Name   string
	Fields []GoModelField
}

// GoModelField describes one JSON field of a connector struct.
type GoModelField struct {
	Name       string
	TypeScript string
	Zod        string
	Optional   bool
}

// LoadGoModels resolves operation types from their connector Go module and follows nested structs.
func LoadGoModels(goDirectory string, typeNames []string) ([]GoModel, error) {
	loaded, err := packages.Load(&packages.Config{Dir: goDirectory, Mode: packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles}, ".")
	if err != nil {
		return nil, fmt.Errorf("load connector Go package: %w", err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("load connector Go package: expected one package, got %d", len(loaded))
	}
	for _, problem := range loaded[0].Errors {
		return nil, fmt.Errorf("load connector Go package: %s", problem)
	}
	packageTypes := loaded[0].Types
	models := make([]GoModel, 0, len(typeNames))
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("recursive Go model %s is unsupported", name)
		}
		object := packageTypes.Scope().Lookup(name)
		if object == nil {
			return fmt.Errorf("Go model %s is missing from %s", name, goDirectory)
		}
		modelType, ok := object.Type().Underlying().(*types.Struct)
		if !ok {
			return fmt.Errorf("Go model %s is not a struct", name)
		}
		visiting[name] = true
		model := GoModel{Name: name, Fields: make([]GoModelField, 0, modelType.NumFields())}
		for index := range modelType.NumFields() {
			field := modelType.Field(index)
			if !field.Exported() {
				continue
			}
			tag := reflect.StructTag(modelType.Tag(index)).Get("json")
			parts := strings.Split(tag, ",")
			if parts[0] == "-" {
				continue
			}
			fieldName := parts[0]
			if fieldName == "" {
				fieldName = field.Name()
			}
			tsType, zod, dependency, err := describeGoModelType(packageTypes, field.Type())
			if err != nil {
				return fmt.Errorf("Go model %s.%s: %w", name, field.Name(), err)
			}
			if dependency != "" && dependency != name {
				if err := visit(dependency); err != nil {
					return err
				}
			}
			optional := false
			for _, option := range parts[1:] {
				if option == "omitempty" {
					optional = true
				}
			}
			_, isPointer := field.Type().(*types.Pointer)
			model.Fields = append(model.Fields, GoModelField{Name: fieldName, TypeScript: tsType, Zod: zod, Optional: optional || isPointer})
		}
		delete(visiting, name)
		visited[name] = true
		models = append(models, model)
		return nil
	}
	for _, name := range typeNames {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return models, nil
}

func describeGoModelType(packageTypes *types.Package, modelType types.Type) (string, string, string, error) {
	switch typed := modelType.(type) {
	case *types.Pointer:
		return describeGoModelType(packageTypes, typed.Elem())
	case *types.Named:
		if typed.Obj().Pkg() != nil && typed.Obj().Pkg().Path() == "time" && typed.Obj().Name() == "Time" {
			return "string", "z.iso.datetime({ offset: true })", "", nil
		}
		if typed.Obj().Pkg() != nil && typed.Obj().Pkg().Path() == "encoding/json" && typed.Obj().Name() == "RawMessage" {
			return "unknown", "z.unknown()", "", nil
		}
		if typed.Obj().Pkg() == packageTypes {
			if _, ok := typed.Underlying().(*types.Struct); ok {
				name := typed.Obj().Name()
				return name, name + "Schema", name, nil
			}
		}
		return describeGoModelType(packageTypes, typed.Underlying())
	case *types.Basic:
		switch {
		case typed.Info()&types.IsString != 0:
			return "string", "z.string()", "", nil
		case typed.Info()&types.IsBoolean != 0:
			return "boolean", "z.boolean()", "", nil
		case typed.Info()&types.IsInteger != 0:
			return "number", "z.number().int()", "", nil
		case typed.Info()&types.IsFloat != 0:
			return "number", "z.number()", "", nil
		}
	case *types.Slice:
		if basic, ok := typed.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
			return "string", "z.string()", "", nil
		}
		elementType, elementSchema, dependency, err := describeGoModelType(packageTypes, typed.Elem())
		return elementType + "[]", "z.array(" + elementSchema + ")", dependency, err
	case *types.Map:
		key, ok := typed.Key().Underlying().(*types.Basic)
		if !ok || key.Kind() != types.String {
			return "", "", "", fmt.Errorf("map key must be string")
		}
		valueType, valueSchema, dependency, err := describeGoModelType(packageTypes, typed.Elem())
		return "Record<string, " + valueType + ">", "z.record(z.string(), " + valueSchema + ")", dependency, err
	case *types.Interface:
		return "unknown", "z.unknown()", "", nil
	}
	return "", "", "", fmt.Errorf("unsupported Go type %s", modelType.String())
}
