// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestTypeScriptGenerationCoversCatalog(t *testing.T) {
	repository := filepath.Join("..", "..")
	source, err := os.ReadFile(filepath.Join(repository, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Directories []string `yaml:"directories"`
	}
	if err := yaml.Unmarshal(source, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, directory := range catalog.Directories {
		t.Run(directory, func(t *testing.T) {
			connectorDirectory := filepath.Join(repository, directory)
			manifestFile, err := os.Open(filepath.Join(connectorDirectory, "connector.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			defer manifestFile.Close()
			manifest, err := schema.Decode(manifestFile)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Spec.Languages == nil {
				manifest.Spec.Languages = &schema.Languages{}
			}
			selected := &schema.LanguagePackage{}
			for _, operation := range manifest.Spec.Operations {
				selected.Operations = append(selected.Operations, operation.Name)
			}
			manifest.Spec.Languages.TypeScript = selected
			generated, err := GenerateTypeScript(manifest, filepath.Join(connectorDirectory, "go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"models.ts", "definitions.ts", "steps.ts", "connection.ts", "mock.ts"} {
				if len(generated[name]) == 0 {
					t.Errorf("empty %s", name)
				}
			}
		})
	}
}

func TestCatalogGeneratedTypeScriptCompiles(t *testing.T) {
	if os.Getenv("TS_CODEGEN_COMPILE") != "1" {
		t.Skip("set TS_CODEGEN_COMPILE=1 after npm ci in connectors/stripe/typescript")
	}
	repository := filepath.Join("..", "..")
	stripeDirectory, err := filepath.Abs(filepath.Join(repository, "connectors", "stripe", "typescript"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(stripeDirectory, "node_modules", ".bin", "tsc")
	if _, err := os.Stat(compiler); err != nil {
		t.Fatal(err)
	}
	generatedRoot, err := os.MkdirTemp(filepath.Join(stripeDirectory, "src"), "catalog-codegen-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(generatedRoot); err != nil {
			t.Error(err)
		}
	})
	source, err := os.ReadFile(filepath.Join(repository, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Directories []string `yaml:"directories"`
	}
	if err := yaml.Unmarshal(source, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, directory := range catalog.Directories {
		connectorDirectory := filepath.Join(repository, directory)
		manifestFile, err := os.Open(filepath.Join(connectorDirectory, "connector.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		manifest, decodeErr := schema.Decode(manifestFile)
		closeErr := manifestFile.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if manifest.Spec.Languages == nil {
			manifest.Spec.Languages = &schema.Languages{}
		}
		selected := &schema.LanguagePackage{}
		for _, operation := range manifest.Spec.Operations {
			selected.Operations = append(selected.Operations, operation.Name)
		}
		manifest.Spec.Languages.TypeScript = selected
		generated, err := GenerateTypeScript(manifest, filepath.Join(connectorDirectory, "go"))
		if err != nil {
			t.Fatalf("%s: %v", directory, err)
		}
		destination := filepath.Join(generatedRoot, filepath.FromSlash(directory))
		if err := os.MkdirAll(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, contents := range generated {
			if err := os.WriteFile(filepath.Join(destination, name), contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	command := exec.Command(compiler, "--noEmit")
	command.Dir = stripeDirectory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("catalog-generated TypeScript did not compile: %v\n%s", err, output)
	}
	var script strings.Builder
	script.WriteString("// Copyright (c) 2026 Super Durable\n// SPDX-License-Identifier: MIT\n\n")
	script.WriteString("for (const path of [\n")
	for _, directory := range catalog.Directories {
		script.WriteString("  " + strconv.Quote("./"+filepath.ToSlash(directory)+"/mock.ts") + ",\n")
	}
	script.WriteString("]) {\n  const mock = await import(path);\n  for (const [name, value] of Object.entries(mock)) {\n    if (/^[a-z]/.test(name) && typeof value === 'function' && value.length === 0) value();\n  }\n}\n")
	if err := os.WriteFile(filepath.Join(generatedRoot, "verify-mocks.mts"), []byte(script.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	verification := exec.Command(filepath.Join(stripeDirectory, "node_modules", ".bin", "tsx"), filepath.Join(generatedRoot, "verify-mocks.mts"))
	verification.Dir = stripeDirectory
	output, err = verification.CombinedOutput()
	if err != nil {
		t.Fatalf("catalog-generated mocks did not parse: %v\n%s", err, output)
	}
}
