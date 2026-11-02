// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

func conformanceOperationsCommand(manifestPath string, output io.Writer) error {
	manifest, err := load(manifestPath)
	if err != nil {
		return err
	}
	type conformanceOperation struct {
		Kind                string   `json:"kind"`
		Branches            []string `json:"branches"`
		OptionalInputFields []string `json:"optionalInputFields"`
	}
	operations := make(map[string]conformanceOperation, len(manifest.Spec.Operations))
	for _, operation := range manifest.Spec.Operations {
		entry := conformanceOperation{Kind: operation.Kind}
		entry.OptionalInputFields, err = conformanceOptionalInputFields(manifestPath, operation.InputType)
		if err != nil {
			return err
		}
		for _, branch := range operation.Branches {
			entry.Branches = append(entry.Branches, branch.ID)
		}
		operations[operation.Name] = entry
	}
	return json.NewEncoder(output).Encode(operations)
}

func conformanceOptionalInputFields(manifestPath, inputType string) ([]string, error) {
	packages, err := parser.ParseDir(token.NewFileSet(), filepath.Join(filepath.Dir(manifestPath), "go"), nil, 0)
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				group, ok := declaration.(*ast.GenDecl)
				if !ok || group.Tok != token.TYPE {
					continue
				}
				for _, specification := range group.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok || typeSpec.Name.Name != inputType {
						continue
					}
					structure, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						if alias, isAlias := typeSpec.Type.(*ast.Ident); isAlias && alias.Name != inputType {
							return conformanceOptionalInputFields(manifestPath, alias.Name)
						}
						return nil, fmt.Errorf("%s is not a struct", inputType)
					}
					optional := []string{}
					for _, field := range structure.Fields.List {
						if field.Tag == nil {
							continue
						}
						tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
						parts := strings.Split(tag.Get("json"), ",")
						if len(parts) > 1 && (slices.Contains(parts[1:], "omitempty") || slices.Contains(parts[1:], "omitzero")) {
							optional = append(optional, parts[0])
						}
					}
					return optional, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("input type %s not found", inputType)
}
