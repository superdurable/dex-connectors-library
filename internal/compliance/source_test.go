// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package compliance_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const licenseIdentifier = "SPDX-License-Identifier: MIT"

var sourceExtensions = map[string]bool{
	".css":  true,
	".go":   true,
	".html": true,
	".py":   true,
	".ts":   true,
	".tsx":  true,
}

func TestHandwrittenPublicGoAPIsHaveDocumentation(t *testing.T) {
	root := repositoryRoot(t)
	var missing []string
	walkFiles(t, root, func(path string) {
		if !isPublicAPISource(root, path) {
			return
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if ast.IsGenerated(file) {
			return
		}
		inspectPublicDeclarations(fileSet, file, path, &missing)
	})
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("hand-written public APIs without documentation:\n%s", strings.Join(missing, "\n"))
	}
}

func TestSourceFilesHaveMITLicenseHeader(t *testing.T) {
	root := repositoryRoot(t)
	var missing []string
	walkFiles(t, root, func(path string) {
		if !sourceExtensions[filepath.Ext(path)] {
			return
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for line := 0; line < 8 && scanner.Scan(); line++ {
			if strings.Contains(scanner.Text(), licenseIdentifier) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		missing = append(missing, relativePath(root, path))
	})
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("source files without an MIT SPDX header:\n%s", strings.Join(missing, "\n"))
	}
}

func inspectPublicDeclarations(fileSet *token.FileSet, file *ast.File, path string, missing *[]string) {
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Name.IsExported() && declaration.Doc == nil {
				appendMissing(fileSet, path, declaration.Pos(), declaration.Name.Name, missing)
			}
		case *ast.GenDecl:
			inspectPublicGeneralDeclaration(fileSet, declaration, path, missing)
		}
	}
}

func inspectPublicGeneralDeclaration(fileSet *token.FileSet, declaration *ast.GenDecl, path string, missing *[]string) {
	for _, specification := range declaration.Specs {
		switch specification := specification.(type) {
		case *ast.TypeSpec:
			if !specification.Name.IsExported() {
				continue
			}
			if specification.Doc == nil && declaration.Doc == nil {
				appendMissing(fileSet, path, specification.Pos(), specification.Name.Name, missing)
			}
			inspectPublicTypeFields(fileSet, specification, path, missing)
		case *ast.ValueSpec:
			for _, name := range specification.Names {
				if name.IsExported() && specification.Doc == nil && specification.Comment == nil && declaration.Doc == nil {
					appendMissing(fileSet, path, name.Pos(), name.Name, missing)
				}
			}
		}
	}
}

func inspectPublicTypeFields(fileSet *token.FileSet, specification *ast.TypeSpec, path string, missing *[]string) {
	var fields *ast.FieldList
	switch expression := specification.Type.(type) {
	case *ast.StructType:
		fields = expression.Fields
	case *ast.InterfaceType:
		fields = expression.Methods
	}
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if name.IsExported() && field.Doc == nil && field.Comment == nil {
				appendMissing(fileSet, path, name.Pos(), specification.Name.Name+"."+name.Name, missing)
			}
		}
	}
}

func appendMissing(fileSet *token.FileSet, path string, position token.Pos, name string, missing *[]string) {
	location := fileSet.Position(position)
	*missing = append(*missing, fmt.Sprintf("%s:%d: %s", path, location.Line, name))
}

func isPublicAPISource(root string, path string) bool {
	relative := filepath.ToSlash(relativePath(root, path))
	if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || strings.Contains(relative, "/internal/") {
		return false
	}
	if strings.HasPrefix(relative, "sdkgo/") {
		return !strings.HasPrefix(relative, "sdkgo/integrationtest/") && !strings.HasPrefix(relative, "sdkgo/testdata/")
	}
	if !strings.HasPrefix(relative, "connectors/") {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(path), "connector.yaml"))
	return err == nil
}

func walkFiles(t *testing.T, root string, visit func(string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "build", "coverage", "vendor":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository sources: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func relativePath(root string, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}
