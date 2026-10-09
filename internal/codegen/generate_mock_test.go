// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

const (
	mocksManifestPath = "../../schema/testdata/mocks.yaml"
	mocksFixturePath  = "testdata/mocks"
	mocksModulePath   = "example.com/mockfixture"
)

func TestGenerateMockPackageMatchesGolden(t *testing.T) {
	files := generateMocksFixture(t)
	for relativePath, generated := range files {
		goldenPath := filepath.Join(mocksFixturePath, relativePath)
		if *shouldUpdateGoldenFiles {
			require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
			require.NoError(t, os.WriteFile(goldenPath, generated, 0o644))
		}
		golden, err := os.ReadFile(goldenPath)
		require.NoError(t, err)
		require.Equal(t, string(golden), string(generated), "run go test ./internal/codegen -run MatchesGolden -update-golden")
	}
}

func TestGenerateMockPackageDeclaresEveryConstructor(t *testing.T) {
	text := string(generateMocksFixture(t)[filepath.Join("mockfixturemock", codegen.MockOutputFile)])

	for _, expected := range []string{
		"package mockfixturemock",
		"func New(t testing.TB, connectionName string) *Mock {",
		"mock.getWidget.Default(GetWidgetDefault())",
		"mock.listWidgets.Default(ListWidgetsDefault())",
		"func (mock *Mock) CreateWidget() *connectormock.Mutation[mockfixture.CreateWidgetInput, mockfixture.Widget]",
		"func GetWidgetNotFound(value mockfixture.Widget, failure *sdkgo.Failure) connectormock.Case[mockfixture.Widget]",
		"func CreateWidgetUncertain(value mockfixture.Widget, failure sdkgo.Failure) connectormock.Case[mockfixture.Widget]",
		"func GetWidgetRetry(failure sdkgo.Failure, after time.Duration) connectormock.Case[mockfixture.Widget]",
		"func GetWidgetMissing() connectormock.Case[mockfixture.Widget]",
		"sdkgo.Failure{Kind: sdkgo.FailureNotFound, Message: \"The widget does not exist.\"}",
		"connectormock.Retry[mockfixture.Widget](sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Message: \"Too many requests.\"}, time.Duration(2000000000))",
		"connectormock.CursorPageNumber[mockfixture.ListWidgetsInput](\"page\", \"nextPage\", pages)",
		"func GetWidgetDefault() connectormock.Case[mockfixture.Widget] { return GetWidgetGear() }",
		`mockfixture.NewConnectionWithOperations(mockOperations{mock: mock}, sdkgo.ConnectionRef{Provider: "example", Name: connectionName})`,
	} {
		require.Contains(t, text, expected)
	}
	require.NotContains(t, text, "CreateWidgetDefault")
	require.Contains(t, text, `"createdAt":"2026-01-02T03:04:05Z"`, "a quoted timestamp stays a JSON string")
	require.Contains(t, text, `"tags":["metal","007"]`, "a quoted number stays a JSON string")
}

func TestGenerateMockPackageCoversEveryFailureKind(t *testing.T) {
	manifest := loadMocksManifest(t)
	for _, kind := range schema.FailureKinds {
		manifest.Spec.Operations[0].Mocks[1].Failure.Kind = kind
		source, _, err := codegen.GenerateMockPackage(manifest, mocksModulePath)
		require.NoError(t, err)
		require.NotContains(t, string(source), "sdkgo.,", kind)
		require.Regexp(t, `Kind: sdkgo\.Failure[A-Za-z]+, Message: "The widget does not exist\."`, string(source))
	}
}

func TestGenerateMockPackageRejectsCollidingIdentifiers(t *testing.T) {
	manifest := loadMocksManifest(t)
	manifest.Spec.Operations[1].GoName = "GetWidgetMissing"
	manifest.Spec.Operations[1].Mocks = nil
	manifest.Spec.Operations[0].Mocks[1].Name = "missingRetry"

	_, _, err := codegen.GenerateMockPackage(manifest, mocksModulePath)

	require.ErrorContains(t, err, "generated mock identifier GetWidgetMissingRetry is declared twice")
}

func TestGenerateMockPackageRequiresMocks(t *testing.T) {
	manifest := loadMocksManifest(t)
	for index := range manifest.Spec.Operations {
		manifest.Spec.Operations[index].Mocks = nil
	}

	_, _, err := codegen.GenerateMockPackage(manifest, mocksModulePath)

	require.ErrorContains(t, err, "declares no mocks")
}

// TestGeneratedMockPackageBehaves compiles the generated connector and mock package against the local SDK source
// and runs the fixture's tests and the generated mock test.
func TestGeneratedMockPackageBehaves(t *testing.T) {
	files := generateMocksFixture(t)
	for _, name := range []string{"client.go", "mock_test.go"} {
		contents, err := os.ReadFile(filepath.Join(mocksFixturePath, name))
		require.NoError(t, err)
		files[name] = contents
	}

	output, err := runFixtureModuleTests(t, mocksModulePath, files)

	require.NoError(t, err, output)
	require.Contains(t, output, "ok  \t"+mocksModulePath+"/mockfixturemock")
}

func generateMocksFixture(t *testing.T) map[string][]byte {
	t.Helper()
	manifest := loadMocksManifest(t)
	connector, err := codegen.Generate(manifest)
	require.NoError(t, err)
	source, test, err := codegen.GenerateMockPackage(manifest, mocksModulePath)
	require.NoError(t, err)
	return map[string][]byte{
		codegen.OutputFile: connector,
		filepath.Join(codegen.MockPackageName(manifest), codegen.MockOutputFile):     source,
		filepath.Join(codegen.MockPackageName(manifest), codegen.MockTestOutputFile): test,
	}
}

func loadMocksManifest(t *testing.T) schema.Manifest {
	t.Helper()
	file, err := os.Open(mocksManifestPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.NoError(t, manifest.ValidateMockCoverage())
	return manifest
}

// runFixtureModuleTests writes files into a temporary module that requires the local SDK source and runs its tests.
func runFixtureModuleTests(t *testing.T, modulePath string, files map[string][]byte) (string, error) {
	t.Helper()
	sdkDirectory, err := filepath.Abs("../../sdkgo")
	require.NoError(t, err)
	sdkModule, err := os.ReadFile(filepath.Join(sdkDirectory, "go.mod"))
	require.NoError(t, err)
	sdkSums, err := os.ReadFile(filepath.Join(sdkDirectory, "go.sum"))
	require.NoError(t, err)
	const sdkModulePath = "github.com/superdurable/dex-connectors-library/sdkgo"
	require.True(t, bytes.HasPrefix(sdkModule, []byte("module "+sdkModulePath+"\n")))
	fixtureModule := "module " + modulePath + "\n" +
		strings.TrimPrefix(string(sdkModule), "module "+sdkModulePath+"\n") +
		"\nrequire " + sdkModulePath + " v0.0.0\n\nreplace " + sdkModulePath + " => " + sdkDirectory + "\n"
	moduleDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.mod"), []byte(fixtureModule), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.sum"), sdkSums, 0o600))
	for relativePath, contents := range files {
		target := filepath.Join(moduleDirectory, relativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, contents, 0o600))
	}
	command := exec.Command("go", "test", "-count=1", "./...")
	command.Dir = moduleDirectory
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestGeneratedMockTestRejectsAnUnknownOutputField(t *testing.T) {
	manifest := loadMocksManifest(t)
	require.NoError(t, manifest.Spec.Operations[0].Mocks[0].Output.UnmarshalJSON([]byte(`{"id": "w-1", "colour": "red"}`)))
	connector, err := codegen.Generate(manifest)
	require.NoError(t, err)
	source, test, err := codegen.GenerateMockPackage(manifest, mocksModulePath)
	require.NoError(t, err)
	client, err := os.ReadFile(filepath.Join(mocksFixturePath, "client.go"))
	require.NoError(t, err)

	output, err := runFixtureModuleTests(t, mocksModulePath, map[string][]byte{
		codegen.OutputFile: connector, "client.go": client,
		filepath.Join("mockfixturemock", codegen.MockOutputFile):     source,
		filepath.Join("mockfixturemock", codegen.MockTestOutputFile): test,
	})

	require.Error(t, err)
	require.Contains(t, output, `manifest mock getWidget/gear/1: decode mockfixture.Widget fixture: json: unknown field "colour"`)
}
