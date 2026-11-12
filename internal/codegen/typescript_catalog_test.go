// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestTypeScriptGenerationCoversCatalog(t *testing.T) {
	for _, connector := range generatedCatalogTypeScript(t) {
		t.Run(connector.directory, func(t *testing.T) {
			if connector.err != nil {
				t.Fatal(connector.err)
			}
			for _, name := range []string{"models.ts", "definitions.ts", "steps.ts", "connection.ts", "mock.ts"} {
				if len(connector.files[name]) == 0 {
					t.Errorf("empty %s", name)
				}
			}
		})
	}
}

// catalogTypeScript is one catalog connector's TypeScript generated for every operation.
type catalogTypeScript struct {
	directory string
	files     map[string][]byte
	err       error
}

var catalogTypeScriptGeneration struct {
	once       sync.Once
	connectors []catalogTypeScript
	err        error
}

// generatedCatalogTypeScript generates the catalog once per test process; both catalog tests check that output.
func generatedCatalogTypeScript(t *testing.T) []catalogTypeScript {
	t.Helper()
	catalogTypeScriptGeneration.once.Do(func() {
		repository := filepath.Join("..", "..")
		source, err := os.ReadFile(filepath.Join(repository, "catalog.yaml"))
		if err != nil {
			catalogTypeScriptGeneration.err = err
			return
		}
		var catalog struct {
			Directories []string `yaml:"directories"`
		}
		if err := yaml.Unmarshal(source, &catalog); err != nil {
			catalogTypeScriptGeneration.err = err
			return
		}
		connectors := make([]catalogTypeScript, len(catalog.Directories))
		indexes := make(chan int)
		var workers sync.WaitGroup
		for range runtime.GOMAXPROCS(0) {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for index := range indexes {
					directory := catalog.Directories[index]
					files, err := generateAllOperationsTypeScript(filepath.Join(repository, directory))
					connectors[index] = catalogTypeScript{directory: directory, files: files, err: err}
				}
			}()
		}
		for index := range catalog.Directories {
			indexes <- index
		}
		close(indexes)
		workers.Wait()
		catalogTypeScriptGeneration.connectors = connectors
	})
	if catalogTypeScriptGeneration.err != nil {
		t.Fatal(catalogTypeScriptGeneration.err)
	}
	return catalogTypeScriptGeneration.connectors
}

// generateAllOperationsTypeScript selects every manifest operation for the TypeScript package.
func generateAllOperationsTypeScript(connectorDirectory string) (map[string][]byte, error) {
	manifestFile, err := os.Open(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		return nil, err
	}
	manifest, decodeErr := schema.Decode(manifestFile)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		return nil, decodeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if manifest.Spec.Languages == nil {
		manifest.Spec.Languages = &schema.Languages{}
	}
	selected := &schema.LanguagePackage{}
	for _, operation := range manifest.Spec.Operations {
		selected.Operations = append(selected.Operations, operation.Name)
	}
	manifest.Spec.Languages.TypeScript = selected
	return GenerateTypeScript(manifest, filepath.Join(connectorDirectory, "go"))
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
	generatedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(generatedRoot, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(stripeDirectory, "node_modules"), filepath.Join(generatedRoot, "node_modules")); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"target":"ES2022","module":"NodeNext","moduleResolution":"NodeNext","strict":true,"exactOptionalPropertyTypes":true,"skipLibCheck":true,"noEmit":true},"include":["connectors/**/*.ts"]}`
	if err := os.WriteFile(filepath.Join(generatedRoot, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := generatedCatalogTypeScript(t)
	for _, connector := range catalog {
		if connector.err != nil {
			t.Fatalf("%s: %v", connector.directory, connector.err)
		}
		destination := filepath.Join(generatedRoot, filepath.FromSlash(connector.directory))
		if err := os.MkdirAll(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, contents := range connector.files {
			if err := os.WriteFile(filepath.Join(destination, name), contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	command := exec.Command(compiler, "--project", generatedRoot)
	command.Dir = generatedRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("catalog-generated TypeScript did not compile: %v\n%s", err, output)
	}
	var script strings.Builder
	script.WriteString("// Copyright (c) 2026 Super Durable\n// SPDX-License-Identifier: MIT\n\n")
	script.WriteString("for (const path of [\n")
	for _, connector := range catalog {
		script.WriteString("  " + strconv.Quote("./"+filepath.ToSlash(connector.directory)+"/mock.ts") + ",\n")
	}
	script.WriteString("]) {\n  const mock = await import(path);\n  for (const [name, value] of Object.entries(mock)) {\n    if (/^[a-z]/.test(name) && typeof value === 'function' && value.length === 0) value();\n  }\n}\n")
	script.WriteString("const { TicketSchema } = await import('./connectors/zendesk/support/models.ts');\n")
	script.WriteString("if (TicketSchema.safeParse({}).success) throw new Error('missing Zendesk status was fabricated');\n")
	script.WriteString("for (const status of ['', 'future-provider-status']) TicketSchema.shape.status.parse(status);\n")
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
