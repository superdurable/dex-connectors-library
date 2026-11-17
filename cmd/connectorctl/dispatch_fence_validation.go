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
	"strings"

	"github.com/superdurable/dex-connectors-library/schema"
)

func validateDispatchFenceSource(manifestPath string, manifest schema.Manifest) error {
	goDirectory := filepath.Join(filepath.Dir(manifestPath), "go")
	entries, err := os.ReadDir(goDirectory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read connector Go sources: %w", err)
	}
	operations := make(map[string]schema.Operation, len(manifest.Spec.Operations))
	for _, operation := range manifest.Spec.Operations {
		operations[operation.GoName] = operation
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasPrefix(entry.Name(), "zz_generated_") {
			continue
		}
		path := filepath.Join(goDirectory, entry.Name())
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		var validationError error
		ast.Inspect(source, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall || len(call.Args) < 2 {
				return true
			}
			function, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector || function.Sel.Name != "NewFence" {
				return true
			}
			packageName, isPackage := function.X.(*ast.Ident)
			if !isPackage || packageName.Name != "singledispatch" {
				return true
			}
			definitionName := fenceDefinitionName(call.Args[1])
			if definitionName == "" {
				return true
			}
			operation, exists := operations[definitionName]
			if exists && !operation.Execution.DispatchFence {
				validationError = fmt.Errorf("%s: singledispatch.NewFence requires execution.dispatchFence: true", operation.Name)
				return false
			}
			return true
		})
		if validationError != nil {
			return validationError
		}
	}
	return nil
}

func fenceDefinitionName(expression ast.Expr) string {
	for {
		selector, isSelector := expression.(*ast.SelectorExpr)
		if !isSelector {
			break
		}
		expression = selector.X
	}
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier || !strings.HasSuffix(identifier.Name, "Definition") {
		return ""
	}
	return strings.TrimSuffix(identifier.Name, "Definition")
}
