// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
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
	cursorType, isStringEncoded, isFound := cursorFieldType(declarations, operation.OutputType, operation.Pagination.NextField)
	if isFound {
		if placeholders, isSupported := placeholdersForCursorType(declarations, cursorType); isSupported {
			if isStringEncoded {
				// Null ends the listing and decodes to zero even with encoding/json's string option.
				return cursorPlaceholders{
					firstNext:  strconv.Quote(placeholders.firstNext),
					secondNext: strconv.Quote(placeholders.secondNext),
					last:       "null",
				}, nil
			}
			return placeholders, nil
		}
	}
	return cursorPlaceholders{}, fmt.Errorf("%s: cannot tell whether the %s field of %s is a string or a number cursor; write the paginated default by hand",
		operation.Name, operation.Pagination.NextField, operation.OutputType)
}

// readTypeDeclarations maps types in the package's active non-test Go files to their expressions.
func readTypeDeclarations(directory string) (map[string]ast.Expr, error) {
	packageFiles, err := build.Default.ImportDir(directory, 0)
	if err != nil {
		var noGoFiles *build.NoGoError
		if !errors.As(err, &noGoFiles) {
			return nil, err
		}
		return map[string]ast.Expr{}, nil
	}
	declarations := map[string]ast.Expr{}
	fileSet := token.NewFileSet()
	for _, name := range append(packageFiles.GoFiles, packageFiles.CgoFiles...) {
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
				if _, exists := declarations[typeSpec.Name.Name]; exists {
					return nil, fmt.Errorf("type %s is declared more than once in active Go files", typeSpec.Name.Name)
				}
				declarations[typeSpec.Name.Name] = typeSpec.Type
			}
		}
	}
	return declarations, nil
}

// cursorFieldType returns the declared type of the exported field of outputType whose JSON name is jsonName.
func cursorFieldType(declarations map[string]ast.Expr, outputType, jsonName string) (ast.Expr, bool, bool) {
	structType, isStruct := declarations[outputType].(*ast.StructType)
	if !isStruct {
		return nil, false, false
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if ast.IsExported(name.Name) {
				matches, isStringEncoded := matchesJSONField(field, name.Name, jsonName)
				if matches {
					return field.Type, isStringEncoded, true
				}
			}
		}
	}
	return nil, false, false
}

// matchesJSONField reports whether encoding/json reads jsonName into the field and applies its string option.
func matchesJSONField(field *ast.Field, goName, jsonName string) (bool, bool) {
	if field.Tag != nil {
		if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
			jsonTag := reflect.StructTag(tag).Get("json")
			tagName, options, _ := strings.Cut(jsonTag, ",")
			if tagName == "-" {
				return false, false
			}
			if tagName != "" {
				return tagName == jsonName, slices.Contains(strings.Split(options, ","), "string")
			}
			return strings.EqualFold(goName, jsonName), slices.Contains(strings.Split(options, ","), "string")
		}
	}
	return strings.EqualFold(goName, jsonName), false
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
