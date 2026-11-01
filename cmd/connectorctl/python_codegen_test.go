// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestPythonCodegenGoldenBitbucketMockUsesProviderIdentity(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket", "connector.yaml")
	manifest, err := load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generatedPythonFiles(manifestPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.py", "definitions.py", "steps.py", "connection.py", "mock.py"} {
		path := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", "atlassian_bitbucket", "_generated", name)
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(files[path], want) {
			t.Errorf("%s differs from generated golden output", name)
		}
	}
	mockPath := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", "atlassian_bitbucket", "_generated", "mock.py")
	mockSource := string(files[mockPath])
	if !strings.Contains(mockSource, `connection_name: str = "bitbucket"`) {
		t.Error("mock default connection name must use metadata.name")
	}
	if !strings.Contains(mockSource, `dex_connectors.atlassian_bitbucket.bitbucketmock`) {
		t.Error("mock helpers must name the real public mock module")
	}
}

func TestPythonCodegenKeepsMockScenariosDistinctFromBranchHelpers(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "google", "calendar")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	operationNames := make([]string, 0, len(manifest.Spec.Operations))
	modelNames := make([]string, 0, len(manifest.Spec.Operations)*2)
	for _, operation := range manifest.Spec.Operations {
		operationNames = append(operationNames, operation.Name)
		modelNames = append(modelNames, operation.InputType, operation.OutputType)
	}
	if manifest.Spec.Languages == nil {
		manifest.Spec.Languages = &schema.Languages{}
	}
	manifest.Spec.Languages.Python = &schema.LanguagePackage{Operations: operationNames, Triggers: []string{}}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), modelNames)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "google_calendar", MockPackageName: "calendarmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	mockSource := generated["mock.py"]
	for _, functionName := range []string{"update_event_not_found", "update_event_not_found_mock"} {
		if strings.Count(mockSource, "def "+functionName+"(") != 1 {
			t.Errorf("expected one %s helper", functionName)
		}
	}
}

func TestPythonCodegenAcceptsOpenEnumsAndMissingMockKeys(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), []string{
		"ListRepositoriesInput", "RepositoryPage", "ListPullRequestsInput", "PullRequestPage",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range graph.Models {
		if graph.Models[index].Name == "RepositoryPage" {
			graph.Models[index].Fields = append(graph.Models[index].Fields, gomodel.Field{
				Name: "state", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open", "closed"}},
			}, gomodel.Field{
				Name: "from", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"source"}},
			})
		}
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "atlassian_bitbucket", MockPackageName: "bitbucketmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated["models.py"], "state: str = _dataclass_field(kw_only=True)") {
		t.Error("required open enum must retain its Go field order as a keyword-only string")
	}
	if strings.Contains(generated["models.py"], "Literal[") {
		t.Error("provider enums are open sets")
	}
	if !strings.Contains(generated["mock.py"], "state=''") {
		t.Error("mock output must supply the Go zero value for an omitted required enum")
	}
	if !strings.Contains(generated["mock.py"], "from_=''") {
		t.Error("mock output must escape a missing required field whose wire name is a Python keyword")
	}
	command := exec.Command("python3", "-c", "import ast, sys; ast.parse(sys.stdin.read())")
	command.Stdin = strings.NewReader(generated["mock.py"])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated mock syntax: %v: %s", err, output)
	}
}

func TestPythonCodegenPreservesOptionalPointerPresence(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), []string{
		"ListRepositoriesInput", "RepositoryPage", "ListPullRequestsInput", "PullRequestPage",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range graph.Models {
		if graph.Models[index].Name != "ListRepositoriesInput" {
			continue
		}
		graph.Models[index].Fields = append(graph.Models[index].Fields,
			gomodel.Field{Name: "isCompleted", Type: gomodel.Type{Kind: gomodel.Bool}, Optional: true, HasPresence: true},
			gomodel.Field{Name: "waitTimeSeconds", Type: gomodel.Type{Kind: gomodel.Int}, Optional: true, HasPresence: true},
			gomodel.Field{Name: "items", Type: gomodel.Type{Kind: gomodel.List, Elem: &gomodel.Type{Kind: gomodel.String}, Nullable: true}, Optional: true, HasPresence: true},
		)
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "atlassian_bitbucket", MockPackageName: "bitbucketmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"is_completed: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isCompleted', 'dex_json_omit_default': True})",
		"wait_time_seconds: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitTimeSeconds', 'dex_json_omit_default': True})",
		"items: tuple[str, ...] | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})",
	} {
		if !strings.Contains(generated["models.py"], expected) {
			t.Errorf("missing pointer presence model field %q", expected)
		}
	}
}

func TestPythonGenerateCheckDetectsModelDrift(t *testing.T) {
	manifestSource := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket", "connector.yaml")
	manifestContents, err := os.ReadFile(manifestSource)
	if err != nil {
		t.Fatal(err)
	}
	projectContents, err := os.ReadFile(filepath.Join(filepath.Dir(manifestSource), "python", "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	connectorDirectory := filepath.Join(repository, "connectors", "atlassian", "bitbucket")
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	packageDirectory := filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "atlassian_bitbucket")
	fixtureFiles := map[string][]byte{
		filepath.Join(repository, "catalog.yaml"): []byte("directories: []\n"),
		manifestPath: manifestContents,
		filepath.Join(connectorDirectory, "go", "go.mod"):             []byte("module example.com/bitbucket\n\ngo 1.24\n"),
		filepath.Join(connectorDirectory, "go", "models.go"):          []byte("package bitbucket\ntype ListRepositoriesInput struct{}\ntype RepositoryPage struct{}\ntype ListPullRequestsInput struct{}\ntype PullRequestPage struct{}\n"),
		filepath.Join(packageDirectory, "__init__.py"):                []byte(""),
		filepath.Join(connectorDirectory, "python", "pyproject.toml"): projectContents,
	}
	for path, contents := range fixtureFiles {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := generate([]string{"--lang", "python", manifestPath}); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--check", "--lang", "python", manifestPath}); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(packageDirectory, "_generated", "models.py")
	if err := os.WriteFile(modelPath, []byte("# drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--lang", "python", "--check", manifestPath}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected Python model drift, got %v", err)
	}
}
