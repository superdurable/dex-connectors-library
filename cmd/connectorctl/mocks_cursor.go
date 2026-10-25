// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/schema"
)

// cursorPlaceholders are the YAML scalars for the next cursors of the first two pages and the ending cursor.
type cursorPlaceholders struct {
	firstNext  string
	secondNext string
	last       string
}

var (
	numberCursorPlaceholders = cursorPlaceholders{firstNext: "2", secondNext: "3", last: "0"}
	stringCursorPlaceholders = cursorPlaceholders{firstNext: `"page-2"`, secondNext: `"page-3"`, last: `""`}
	numberCursorTypeNames    = []string{"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64"}
)

// scaffoldCursorPlaceholders returns placeholders of the Go type that the output's next cursor field declares.
func scaffoldCursorPlaceholders(connectorDirectory string, operation schema.Operation) (cursorPlaceholders, error) {
	declarations, err := readTypeDeclarations(connectorDirectory)
	if err != nil {
		return cursorPlaceholders{}, fmt.Errorf("%s: read the Go types of the pagination cursor: %w", operation.Name, err)
	}
	cursorType, isFound := cursorFieldType(declarations, operation.OutputType, operation.Pagination.NextField)
	if isFound {
		if placeholders, isSupported := placeholdersForCursorType(declarations, cursorType); isSupported {
			return placeholders, nil
		}
	}
	return cursorPlaceholders{}, fmt.Errorf("%s: cannot tell whether the %s field of %s is a string or a number cursor; write the paginated default by hand",
		operation.Name, operation.Pagination.NextField, operation.OutputType)
}

// readTypeDeclarations maps each type declared by the package's non-test Go files to its type expression.
func readTypeDeclarations(directory string) (map[string]ast.Expr, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	declarations := map[string]ast.Expr{}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				declarations[typeSpec.Name.Name] = typeSpec.Type
			}
		}
	}
	return declarations, nil
}

// cursorFieldType returns the declared type of the exported field of outputType whose JSON name is jsonName.
func cursorFieldType(declarations map[string]ast.Expr, outputType, jsonName string) (ast.Expr, bool) {
	structType, isStruct := declarations[outputType].(*ast.StructType)
	if !isStruct {
		return nil, false
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if ast.IsExported(name.Name) && hasJSONName(field, name.Name, jsonName) {
				return field.Type, true
			}
		}
	}
	return nil, false
}

// hasJSONName reports whether encoding/json reads jsonName into the field, by tag or case-insensitive Go name.
func hasJSONName(field *ast.Field, goName, jsonName string) bool {
	if field.Tag != nil {
		if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
			if tagName, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ","); tagName != "" {
				return tagName == jsonName
			}
		}
	}
	return strings.EqualFold(goName, jsonName)
}

// placeholdersForCursorType follows pointers and local named types to a string or a number.
func placeholdersForCursorType(declarations map[string]ast.Expr, cursorType ast.Expr) (cursorPlaceholders, bool) {
	const maxNamedTypeDepth = 8
	for range maxNamedTypeDepth {
		switch typed := cursorType.(type) {
		case *ast.StarExpr:
			cursorType = typed.X
		case *ast.Ident:
			switch {
			case typed.Name == "string":
				return stringCursorPlaceholders, true
			case slices.Contains(numberCursorTypeNames, typed.Name):
				return numberCursorPlaceholders, true
			}
			underlying, isDeclared := declarations[typed.Name]
			if !isDeclared {
				return cursorPlaceholders{}, false
			}
			cursorType = underlying
		default:
			return cursorPlaceholders{}, false
		}
	}
	return cursorPlaceholders{}, false
}
