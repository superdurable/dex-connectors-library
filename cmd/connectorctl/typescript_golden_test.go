// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonLegacyTypeScriptGoldenDefaultGeneration(t *testing.T) {
	fixture := filepath.Join("testdata", "typescript-golden")
	repository := t.TempDir()
	if err := filepath.WalkDir(fixture, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(fixture, source)
		if err != nil {
			return err
		}
		destination := filepath.Join(repository, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, contents, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	connector := filepath.Join("connectors", "hackernews")
	manifest := filepath.Join(repository, connector, "connector.yaml")
	if err := generate([]string{manifest}); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--check", manifest}); err != nil {
		t.Fatal(err)
	}
	generatedDirectory := filepath.Join(repository, connector, "typescript", "src", "generated")
	for _, name := range []string{"models.ts", "definitions.ts", "steps.ts", "connection.ts", "mock.ts"} {
		want, err := os.ReadFile(filepath.Join(fixture, connector, "typescript", "src", "generated", name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(generatedDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from golden output", name)
		}
	}
	goOutput := filepath.Join(repository, connector, "go", "zz_generated_connector.go")
	goContents, err := os.ReadFile(goOutput)
	if err != nil {
		t.Fatal(err)
	}
	markedGoContents := append([]byte("// untouched by TypeScript-only generation\n"), goContents...)
	if err := os.WriteFile(goOutput, markedGoContents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--lang", "typescript", manifest}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := os.ReadFile(goOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, markedGoContents) {
		t.Error("--lang typescript modified Go output")
	}
	if err := generate([]string{"--check", "--lang", "typescript", manifest}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TS_CODEGEN_COMPILE") != "1" {
		return
	}
	stripeDirectory, err := filepath.Abs(filepath.Join("..", "..", "connectors", "stripe", "typescript"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(stripeDirectory, "node_modules", ".bin", "tsc")
	if _, err := os.Stat(compiler); err != nil {
		t.Fatal(err)
	}
	compileDirectory, err := os.MkdirTemp(filepath.Join(stripeDirectory, "src"), "golden-codegen-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(compileDirectory); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"models.ts", "definitions.ts", "steps.ts", "connection.ts", "mock.ts"} {
		contents, err := os.ReadFile(filepath.Join(generatedDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(compileDirectory, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(compiler, "--noEmit")
	command.Dir = stripeDirectory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("golden TypeScript compilation: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}
