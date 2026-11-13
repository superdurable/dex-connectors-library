// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorTestSelectionAcceptsRelativeCatalogPath(t *testing.T) {
	selected, err := selectConnectorTestDirectories(
		filepath.Join("..", "..", "catalog.yaml"),
		[]string{"connectors/example"},
		"changed",
		"HEAD",
		"HEAD",
	)
	require.NoError(t, err)
	require.Empty(t, selected)
}

func TestConnectorTestSelectionUsesLocalAndCatalogChanges(t *testing.T) {
	directories := []string{
		"connectors/acme/chat",
		"connectors/acme/mail",
		"connectors/other/payments",
	}
	testCases := []struct {
		name     string
		previous []string
		paths    []string
		expected []string
	}{
		{
			name: "one connector", previous: directories,
			paths: []string{"connectors/acme/mail/client.go"}, expected: []string{"connectors/acme/mail"},
		},
		{
			name: "one connector conformance fixture", previous: directories,
			paths: []string{"conformance/connectors/acme/mail/scenarios.json"}, expected: []string{"connectors/acme/mail"},
		},
		{
			name: "catalog addition", previous: directories[:2],
			paths: []string{"catalog.yaml"}, expected: []string{"connectors/other/payments"},
		},
		{
			name: "deleted connector", previous: append(directories, "connectors/removed/chat"),
			paths: []string{"catalog.yaml", "connectors/removed/chat/client.go"}, expected: []string{},
		},
		{
			name: "shared source", previous: directories,
			paths: []string{"schema/manifest.go"}, expected: directories,
		},
		{
			name: "shared React", previous: directories,
			paths: []string{"sdk/react/src/index.ts"}, expected: directories,
		},
		{
			name: "catalog tooling", previous: directories,
			paths: []string{"cmd/connectorctl/catalog.go"}, expected: directories,
		},
		{
			name: "conformance runner", previous: directories,
			paths: []string{"script/connector_conformance.py"}, expected: directories,
		},
		{
			name: "workflow", previous: directories,
			paths: []string{".github/workflows/ci.yml"}, expected: directories,
		},
		{
			name: "release-only scripts and their tests", previous: directories,
			paths: []string{"script/release/language_publish.py", "script/release/component_release_test.py", "script/agent_rules_test.py"}, expected: []string{},
		},
		{
			name: "release script and one connector", previous: directories,
			paths: []string{"script/release/npm_publish.py", "connectors/acme/mail/client.go"}, expected: []string{"connectors/acme/mail"},
		},
		{
			name: "connector validation script", previous: directories,
			paths: []string{"script/release/component_release.py"}, expected: directories,
		},
		{
			name: "release script and code generation", previous: directories,
			paths: []string{"script/release/language_publish.py", "internal/codegen/generator.go"}, expected: directories,
		},
		{
			name: "unlisted release script", previous: directories,
			paths: []string{"script/release/new_tool.py"}, expected: directories,
		},
		{
			name: "documentation", previous: directories,
			paths: []string{"docs/architecture.md", "connectors/acme/logo.svg"}, expected: []string{},
		},
		{
			name: "Connectors MCP server", previous: directories,
			paths: []string{"connectorsmcp/go.mod", "connectorsmcp/internal/server/server.go"}, expected: []string{},
		},
		{
			name: "directory site and agent rules", previous: directories,
			paths: []string{"site/src/App.tsx", "AGENTS.md", ".cursor/rules/project-core.mdc"}, expected: []string{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			selected := selectConnectorDirectoriesForPaths(directories, testCase.previous, testCase.paths, "catalog.yaml")
			require.Equal(t, testCase.expected, selected)
		})
	}
}

func TestConnectorTestSelectionUsesLongestRegisteredDirectory(t *testing.T) {
	directories := []string{"connectors/acme", "connectors/acme/chat"}
	selected := selectConnectorDirectoriesForPaths(
		directories,
		directories,
		[]string{"connectors/acme/chat/client.go"},
		"catalog.yaml",
	)
	require.Equal(t, []string{"connectors/acme/chat"}, selected)
}

func TestConnectorTestMatrixCoversEveryDirectoryOnce(t *testing.T) {
	for _, connectorCount := range []int{1, 256, 513} {
		t.Run(fmt.Sprintf("%d connectors", connectorCount), func(t *testing.T) {
			directories := make([]string, 0, connectorCount)
			for index := range connectorCount {
				directories = append(directories, fmt.Sprintf("connectors/company/connector-%04d", index))
			}
			matrix := buildConnectorTestMatrix(directories, maximumGitHubActionsMatrixJobs)
			require.LessOrEqual(t, len(matrix.Include), maximumGitHubActionsMatrixJobs)
			expectedShardCount := min(connectorCount, maximumGitHubActionsMatrixJobs)
			seen := map[string]bool{}
			for _, shard := range matrix.Include {
				require.Equal(t, expectedShardCount, shard.Count)
				for _, directory := range directoriesInConnectorTestShard(directories, shard.Index, shard.Count) {
					require.False(t, seen[directory], directory)
					seen[directory] = true
				}
			}
			require.Len(t, seen, len(directories))
			require.Equal(t, matrix, buildConnectorTestMatrix(directories, maximumGitHubActionsMatrixJobs))
		})
	}
}

func TestEmptyConnectorTestMatrixHasNoJobs(t *testing.T) {
	require.Equal(t, connectorTestMatrix{Include: []connectorTestShard{}}, buildConnectorTestMatrix(nil, 256))
}

func TestCIJobsSelectOnlyChangedInputs(t *testing.T) {
	allJobs := selectAllCIJobs()
	python := ciJobSelection{CommonPythonScripts: true}
	pythonAndRootGo := ciJobSelection{CommonPythonScripts: true, CommonRootGo: true}
	connectorInputs := pythonAndRootGo
	sdkgo := ciJobSelection{CommonSDKGo: true, CommonRootGo: true, CommonPythonScripts: true, SDKPython: true, SDKTypeScript: true, SDKGoIntegration: true, ConnectorsMCP: true}
	testCases := []struct {
		name     string
		path     string
		expected ciJobSelection
	}{
		{"Makefile", "Makefile", allJobs},
		{"script", "script/x.py", allJobs},
		{"workflow", ".github/workflows/ci.yml", allJobs},
		{"other workflow", ".github/workflows/x.yml", allJobs},
		{"composite action", ".github/actions/x/action.yml", allJobs},
		{"CI job planner", "cmd/connectorctl/test_matrix.go", allJobs},
		{"CI summary", "script/ci_summary.py", allJobs},
		{"CI summary test", "script/ci_summary_test.py", python},
		{"Go test cache mtime script", "script/set_mtimes_from_git_objects.py", allJobs},
		{"Go test cache mtime script test", "script/set_mtimes_from_git_objects_test.py", python},
		{"schema", "schema/x", allJobs},
		{"internal", "internal/x.go", allJobs},
		{"command", "cmd/connectorctl/x.go", allJobs},
		{"root Go module", "go.mod", allJobs},
		{"root Go checksums", "go.sum", allJobs},
		{"code generation", "internal/codegen/x.go", allJobs},
		{"connector validation script", "script/release/component_release.py", allJobs},
		{"release inventory loaded by connector validation", "script/release/github_release_inventory.py", allJobs},
		{"conformance runner", "script/connector_conformance.py", allJobs},
		{"conformance replay runner", "conformance/runners/python/x.py", allJobs},
		{"TypeScript conformance runner", "conformance/runners/typescript/connector_scenarios.ts", allJobs},
		{"shared conformance cases", "conformance/runners/clock_cases.json", allJobs},
		{"Dex compatibility checks", "script/dex_compatibility.py", allJobs},
		{"per-package Python check", "script/python_model_drift_test.py", allJobs},
		{"per-package TypeScript check", "script/typescript_input_schema_test.mts", allJobs},
		{"unlisted release script", "script/release/new_tool.py", allJobs},
		{"release workflow", ".github/workflows/release-connector.yml", allJobs},
		{"unknown root file", "renovate.json", allJobs},
		{"unknown directory", "tools/x.go", allJobs},
		{"Dex compatibility fixture", "test/dexcompat/x.go", allJobs},
		{"release script test", "script/release/component_release_test.py", python},
		{"language publish script run by root Go tests", "script/release/language_publish.py", pythonAndRootGo},
		{"timestamp parity test run by root Go tests", "script/python_timestamp_parity_test.py", pythonAndRootGo},
		{"npm publish script", "script/release/npm_publish.py", python},
		{"release retitle script", "script/release/retitle_connector_releases.py", python},
		{"Connectors MCP release script", "script/release/connectors_mcp_release.py", python},
		{"CI workflow test", "script/ci_workflow_test.py", python},
		{"branch policy hook", "script/branch_off_main_policy.py", python},
		{"commit message hook", ".githooks/commit-msg", python},
		{"pull request template", ".github/PULL_REQUEST_TEMPLATE.md", python},
		{"agent rules", "AGENTS.md", python},
		{"Claude rules", "CLAUDE.md", python},
		{"Cursor rules", ".cursor/rules/x.mdc", python},
		{"Codex rules", ".codex/rules/x.md", python},
		{"documentation", "docs/x.md", python},
		{"documentation image", "docs/x.png", python},
		{"source file under docs", "docs/x.ts", python},
		{"hook source under agent rules", ".cursor/hooks/x.py", python},
		{"examples", "examples/x/main.go", allJobs},
		{"README", "README.md", python},
		{"root conformance", "conformance/x.json", allJobs},
		{"connector conformance", "conformance/connectors/x/scenarios.json", connectorInputs},
		{"Airtable conformance read by both clock tests", "conformance/connectors/airtable/scenarios.json",
			ciJobSelection{CommonRootGo: true, CommonPythonScripts: true, CommonTypeScriptConformance: true}},
		{"Go SDK", "sdkgo/x.go", sdkgo},
		{"Go SDK module", "sdkgo/go.mod", sdkgo},
		{"Connectors MCP server", "connectorsmcp/internal/server/server.go", ciJobSelection{ConnectorsMCP: true, CommonPythonScripts: true}},
		{"Connectors MCP release config", "connectorsmcp/release.yaml", ciJobSelection{ConnectorsMCP: true, CommonPythonScripts: true}},
		{"Python SDK", "sdkpython/src/sdk.py", ciJobSelection{SDKPython: true, CommonPythonScripts: true}},
		{"TypeScript SDK", "sdkts/src/sdk.ts", ciJobSelection{SDKTypeScript: true, CommonPythonScripts: true}},
		{"React SDK", "sdk/react/src/index.ts", ciJobSelection{React: true, CommonPythonScripts: true}},
		{"Studio theme read by the bundle theme check", "sdk/react/src/studio-theme.ts", ciJobSelection{React: true, CommonPythonScripts: true}},
		{"site", "site/src/App.tsx", ciJobSelection{Site: true}},
		{"catalog", "catalog.yaml", ciJobSelection{Site: true, CommonRootGo: true}},
		{"Stripe TypeScript", "connectors/stripe/typescript/x.ts", ciJobSelection{CommonRootGo: true, CommonPythonScripts: true, CommonTypeScriptConformance: true}},
		{"Stripe TypeScript lockfile", "connectors/stripe/typescript/package-lock.json", ciJobSelection{CommonRootGo: true, CommonPythonScripts: true, CommonTypeScriptConformance: true}},
		{"Gmail TypeScript", "connectors/google/gmail/typescript/package-lock.json", connectorInputs},
		{"Stripe Go", "connectors/stripe/go/x.go", connectorInputs},
		{"connector Go", "connectors/foo/go/x.go", connectorInputs},
		{"connector manifest", "connectors/foo/connector.yaml", connectorInputs},
		{"nested connector manifest", "connectors/google/gmail/connector.yaml", connectorInputs},
		{"connector README", "connectors/foo/README.md", connectorInputs},
		{"connector logo", "connectors/foo/logo.svg", connectorInputs},
		{"gitignore", ".gitignore", ciJobSelection{}},
		{"code of conduct", "CODE_OF_CONDUCT.md", ciJobSelection{}},
		{"contributing", "CONTRIBUTING.md", ciJobSelection{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, selectCIJobsForPaths("changed", []string{testCase.path}))
		})
	}
	require.Equal(t, allJobs, selectCIJobsForPaths("all", nil))
	require.Equal(t, ciJobSelection{CommonPythonScripts: true, CommonRootGo: true, Site: true}, selectCIJobsForPaths("changed", []string{"AGENTS.md", "catalog.yaml"}))
	require.Equal(t, allJobs, selectCIJobsForPaths("changed", []string{"script/release/language_publish.py", "internal/codegen/x.go"}))
	require.Equal(t, ciJobSelection{CommonPythonScripts: true, React: true}, selectCIJobsForPaths("changed", []string{"script/release/npm_publish.py", "sdk/react/src/index.ts"}))
	require.Equal(t, ciJobSelection{React: true, CommonPythonScripts: true}, selectCIJobsForPaths("changed", []string{"sdk/react/src/studio-theme.ts"}))
	require.Equal(t, sdkgo, selectCIJobsForPaths("changed", []string{"sdkgo/step.go", "docs/architecture.md"}))
}

func TestSiteAndDocumentationOnlyChangesSkipEveryPathSelectedCommonBlock(t *testing.T) {
	for _, paths := range [][]string{
		{"site/src/App.tsx", "site/package.json", "site/src/styles.css"},
		{".gitignore", "CONTRIBUTING.md", "CODE_OF_CONDUCT.md"},
	} {
		selection := selectCIJobsForPaths("changed", paths)
		require.False(t, selection.CommonSDKGo || selection.CommonRootGo || selection.CommonPythonScripts || selection.CommonTypeScriptConformance, "%v selected %+v", paths, selection)
	}
	// The Python suite reads agent-rule docs; the Go and TypeScript blocks do not.
	selection := selectCIJobsForPaths("changed", []string{".gitignore", "docs/versioning-and-releases.md", "site/src/App.tsx"})
	require.Equal(t, ciJobSelection{CommonPythonScripts: true, Site: true}, selection)
}

// Root-module repository checks read every source file and manifest, so they are not path-selected.
func TestRepositoryWideChecksRunOnEveryChange(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	require.NoError(t, err)
	require.Contains(t, string(makefile), "ROOT_GO_REPOSITORY_CHECK_PACKAGES = ./internal/compliance ./schema\n")
	require.Contains(t, string(makefile), "ROOT_GO_REPOSITORY_CHECK_TESTS = ^TestRepositoryToolingTestOnlyPathsExistAndNeverRunPerConnector$$\n")
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	job := regexp.MustCompile(`(?s)\n  common-repository-checks:\n(.*?)\n\n  [a-z]`).FindSubmatch(workflow)
	require.NotNil(t, job)
	require.NotContains(t, string(job[1]), "\n    if:", "repository-wide checks run on every event and change")
	require.Contains(t, string(job[1]), "- run: make test-common-repository-checks\n")
	// Manifests and Go sources also select the slower root-module suites that read them.
	for _, path := range []string{"connectors/acme/connector.yaml", "connectors/acme/go/new.go", "connectors/google/gmail/go/oauth.go",
		"conformance/connectors/acme/scenarios.json", "catalog.yaml"} {
		require.True(t, changedPathTestImpact(path).ciJobs.CommonRootGo, path)
	}
}

func TestCIJobGitHubOutputsNameEveryCommonBlock(t *testing.T) {
	require.Equal(t,
		"common_sdkgo=true\ncommon_root_go=false\ncommon_python_scripts=true\ncommon_typescript_conformance=false\nsdkpython=false\nsdkts=false\nsdkgo_integration=false\nreact=false\nsite=true\nconnectors_mcp=false\n",
		ciJobSelection{CommonSDKGo: true, CommonPythonScripts: true, Site: true}.githubOutputLines())
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	for _, line := range strings.Split(strings.TrimSpace(selectAllCIJobs().githubOutputLines()), "\n") {
		name := strings.SplitN(line, "=", 2)[0]
		require.Contains(t, string(workflow), name+": ${{ steps.plan.outputs."+name+" }}", "connector-plan must export %s", name)
	}
}

func TestRepositoryToolingTestOnlyPathsExistAndNeverRunPerConnector(t *testing.T) {
	for path := range repositoryToolingTestOnlyPaths {
		_, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(path)))
		require.NoError(t, err, "remove the stale classification for %s", path)
		require.False(t, changedPathTestImpact(path).shouldTestAllConnectors, path)
	}
	repositoryRoot := filepath.Join("..", "..")
	trackedFiles, err := gitOutput(repositoryRoot, "ls-files", "-z")
	require.NoError(t, err)
	contentsByPath := map[string]string{}
	for _, path := range splitNullTerminated(trackedFiles) {
		if !isToolingConsumerCandidatePath(path) {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(path)))
		require.NoError(t, err)
		contentsByPath[path] = string(contents)
	}
	require.Contains(t, contentsByPath, "Makefile")
	require.Contains(t, contentsByPath, ".github/workflows/ci.yml")
	references := findUnlistedToolingReferencesInFiles(repositoryToolingTestOnlyPaths, contentsByPath)
	unreviewed := []string{}
	for _, reference := range references {
		if !reviewedNonConnectorToolingReferences[reference] {
			unreviewed = append(unreviewed, reference)
		}
	}
	// A consumer that runs in connector CI must take the listed file off repositoryToolingTestOnlyPaths.
	require.Empty(t, unreviewed)
	for reference := range reviewedNonConnectorToolingReferences {
		require.Contains(t, references, reference, "remove the stale reviewed reference")
	}
	// A root-module Go test that runs listed tooling must make that file select the root Go block.
	for _, reference := range references {
		consumerPath, moduleName, _ := strings.Cut(reference, " references ")
		if !isRootModuleGoSource(consumerPath) {
			continue
		}
		for listedPath := range repositoryToolingTestOnlyPaths {
			if strings.TrimSuffix(filepath.Base(listedPath), filepath.Ext(listedPath)) == moduleName {
				require.True(t, repositoryToolingRootGoTestInputs[listedPath], "%s: add %s to repositoryToolingRootGoTestInputs", reference, listedPath)
				require.True(t, changedPathTestImpact(listedPath).ciJobs.CommonRootGo, listedPath)
			}
		}
	}
	for listedPath := range repositoryToolingRootGoTestInputs {
		require.True(t, repositoryToolingTestOnlyPaths[listedPath], "%s must also be listed repository tooling", listedPath)
	}
}

func isRootModuleGoSource(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	for _, prefix := range []string{"cmd/", "internal/", "schema/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func TestToolingConsumerGuardRejectsSyntheticConsumers(t *testing.T) {
	listed := map[string]bool{"script/release/language_publish.py": true, "script/release/language_publish_test.py": true}
	testCases := []struct {
		name     string
		path     string
		contents string
		expected []string
	}{
		{"Python package import", "script/connector_conformance.py", "from script.release import language_publish\n",
			[]string{"script/connector_conformance.py references language_publish"}},
		{"Python sys.path import", "conformance/runners/python/x.py", "import language_publish as publish\n",
			[]string{"conformance/runners/python/x.py references language_publish"}},
		{"Go command", "cmd/connectorctl/x.go", `exec.Command("python3", filepath.Join("script", "release", "language_publish.py"))`,
			[]string{"cmd/connectorctl/x.go references language_publish"}},
		{"shell wrapper", "script/x.sh", "python3 \"$root/script/release/language_publish.py\"\n",
			[]string{"script/x.sh references language_publish"}},
		{"other workflow", ".github/workflows/other.yml", "run: python3 script/release/language_publish.py\n",
			[]string{".github/workflows/other.yml references language_publish"}},
		{"connector test", "connectors/acme/python/tests/test_x.py", "import language_publish\n",
			[]string{"connectors/acme/python/tests/test_x.py references language_publish"}},
		{"GNUmakefile", "GNUmakefile", "test-typescript-connectors:\n\tpython3 script/release/language_publish.py select\n",
			[]string{"GNUmakefile references language_publish"}},
		{"extensionless include", "ci-targets", "test-release-tool:\n\tpython3 script/release/language_publish.py select\n",
			[]string{"ci-targets references language_publish"}},
		{"composite action", ".github/actions/connector-check/action.yml", "runs:\n  steps:\n    - run: python3 script/release/language_publish.py\n",
			[]string{".github/actions/connector-check/action.yml references language_publish"}},
		{"YAML outside .github", "tools/connector-check/action.yaml", "run: python3 script/release/language_publish.py\n",
			[]string{"tools/connector-check/action.yaml references language_publish"}},
		{"non-candidate prose", "docs/x.md", "language_publish.py publishes packages.\n", []string{}},
		{"Makefile outside test-common-python-scripts", "Makefile", "test-x:\n\tpython3 script/release/language_publish.py check\n",
			[]string{"Makefile references language_publish: \tpython3 script/release/language_publish.py check"}},
		{"Makefile unittest prefix outside test-common-python-scripts", "Makefile",
			"test-typescript-connectors:\n\tpython3 -m unittest script/release/language_publish_test.py && python3 script/release/language_publish.py select\n",
			[]string{
				"Makefile references language_publish: \tpython3 -m unittest script/release/language_publish_test.py && python3 script/release/language_publish.py select",
				"Makefile references language_publish_test: \tpython3 -m unittest script/release/language_publish_test.py && python3 script/release/language_publish.py select",
			}},
		{"Makefile test-common-python-scripts shell operator", "Makefile",
			"test-common-python-scripts:\n\tpython3 -m unittest script/release/language_publish_test.py; python3 script/release/language_publish.py select\n",
			[]string{
				"Makefile references language_publish: \tpython3 -m unittest script/release/language_publish_test.py; python3 script/release/language_publish.py select",
				"Makefile references language_publish_test: \tpython3 -m unittest script/release/language_publish_test.py; python3 script/release/language_publish.py select",
			}},
		{"Makefile test-common-python-scripts continuation", "Makefile",
			"test-common-python-scripts:\n\tpython3 -m unittest script/release/language_publish_test.py \\\n\t\t$(shell python3 script/release/language_publish.py)\n",
			[]string{
				"Makefile references language_publish: \tpython3 -m unittest script/release/language_publish_test.py  \t\t$(shell python3 script/release/language_publish.py)",
				"Makefile references language_publish_test: \tpython3 -m unittest script/release/language_publish_test.py  \t\t$(shell python3 script/release/language_publish.py)",
			}},
		{"Makefile test-common-python-scripts non-test argument", "Makefile", "test-common-python-scripts:\n\tpython3 -m unittest script/release/language_publish.py\n",
			[]string{"Makefile references language_publish: \tpython3 -m unittest script/release/language_publish.py"}},
		{"Makefile variable", "Makefile", "TOOL := script/release/language_publish.py\ntest-common-python-scripts:\n\tpython3 -m unittest script/release/language_publish_test.py\n",
			[]string{"Makefile references language_publish: TOOL := script/release/language_publish.py"}},
		{"Makefile test-common-python-scripts", "Makefile", "test-common-python-scripts:\n\tcd sdkgo && go test ./...\n\tpython3 -m unittest script/release/language_publish_test.py script/x_test.py\n", []string{}},
		{"Makefile aggregate test-common", "Makefile", "test-common:\n\tpython3 -m unittest script/release/language_publish_test.py\n",
			[]string{"Makefile references language_publish_test: \tpython3 -m unittest script/release/language_publish_test.py"}},
		{"own test", "script/release/language_publish_test.py", "from script.release import language_publish\n", []string{}},
		{"longer identifier", "script/x.py", "def validate_language_publish_manifest(): pass\n", []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, findUnlistedToolingReferencesInFiles(listed, map[string]string{testCase.path: testCase.contents}))
		})
	}
}

// reviewedNonConnectorToolingReferences name listed tooling from files that never run in connector CI.
var reviewedNonConnectorToolingReferences = map[string]bool{
	// Local hook installer, run only by `make githooks`.
	"script/install-githooks references commit-msg": true,
	// Local Codex and Cursor branch hooks.
	".codex/hooks/validate-branch-off-main.sh references branch_off_main_policy":  true,
	".cursor/hooks/validate-branch-off-main.sh references branch_off_main_policy": true,
	// Release and token-check workflows, which CI never runs.
	".github/workflows/check-npm-token.yml references npm_publish":              true,
	".github/workflows/release-candidate.yml references npm_publish":            true,
	".github/workflows/release-connector.yml references connectors_mcp_release": true,
	".github/workflows/release-connector.yml references language_publish":       true,
	".github/workflows/release-connector.yml references npm_publish":            true,
	".github/workflows/release-sdk-ts.yml references npm_publish":               true,
	// Comments only.
	"cmd/connectorctl/catalog.go references language_publish": true,
	// Root-module Go tests, which run only in common.
	"cmd/connectorctl/python_codegen_test.go references python_timestamp_parity_test": true,
	"cmd/connectorctl/release_selection_test.go references language_publish":          true,
}

func isToolingConsumerCandidatePath(path string) bool {
	// Scan every file except prose and images, so new build inputs such as GNUmakefile or includes are covered.
	for _, extension := range []string{".md", ".mdx", ".mdc", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp"} {
		if strings.HasSuffix(strings.ToLower(path), extension) {
			return false
		}
	}
	return true
}

// findUnlistedToolingReferencesInFiles applies the candidate filter that the repository scan uses.
func findUnlistedToolingReferencesInFiles(listedPaths map[string]bool, contentsByPath map[string]string) []string {
	candidates := map[string]string{}
	for path, contents := range contentsByPath {
		if isToolingConsumerCandidatePath(path) {
			candidates[path] = contents
		}
	}
	return findUnlistedToolingReferences(listedPaths, candidates)
}

// findUnlistedToolingReferences reports unlisted, non-classifier files naming a listed module, except the Python suite's unittest line.
func findUnlistedToolingReferences(listedPaths map[string]bool, contentsByPath map[string]string) []string {
	references := []string{}
	for consumerPath, contents := range contentsByPath {
		if listedPaths[consumerPath] || consumerPath == "cmd/connectorctl/test_matrix.go" || consumerPath == "cmd/connectorctl/test_matrix_test.go" {
			continue
		}
		for listedPath := range listedPaths {
			moduleName := strings.TrimSuffix(filepath.Base(listedPath), filepath.Ext(listedPath))
			if !containsToolingIdentifier(contents, moduleName) {
				continue
			}
			if consumerPath != "Makefile" {
				references = append(references, consumerPath+" references "+moduleName)
				continue
			}
			for _, line := range makefileLogicalLines(contents) {
				if containsToolingIdentifier(line.text, moduleName) && !isPythonScriptsUnittestLine(line) {
					references = append(references, "Makefile references "+moduleName+": "+line.text)
				}
			}
		}
	}
	sort.Strings(references)
	return references
}

// pythonScriptsUnittestLine admits only plain test-file arguments, so no shell operator can run another script.
var pythonScriptsUnittestLine = regexp.MustCompile(`^\tpython3 -m unittest( [A-Za-z0-9_./-]+_test\.py)+$`)

var makefileRuleLine = regexp.MustCompile(`^([^\t#=:][^=:]*):([^=]|$)`)

type makefileLogicalLine struct {
	target string
	text   string
}

func isPythonScriptsUnittestLine(line makefileLogicalLine) bool {
	return line.target == "test-common-python-scripts" && pythonScriptsUnittestLine.MatchString(line.text)
}

// makefileLogicalLines joins backslash continuations and records the rule each recipe line belongs to.
func makefileLogicalLines(contents string) []makefileLogicalLine {
	lines := []makefileLogicalLine{}
	target := ""
	pending := ""
	for _, physicalLine := range strings.Split(contents, "\n") {
		if strings.HasSuffix(physicalLine, "\\") {
			pending += strings.TrimSuffix(physicalLine, "\\") + " "
			continue
		}
		text := pending + physicalLine
		pending = ""
		if strings.HasPrefix(text, "\t") {
			lines = append(lines, makefileLogicalLine{target: target, text: text})
			continue
		}
		target = ""
		if match := makefileRuleLine.FindStringSubmatch(text); match != nil {
			target = strings.TrimSpace(match[1])
		}
		lines = append(lines, makefileLogicalLine{text: text})
	}
	if pending != "" {
		lines = append(lines, makefileLogicalLine{target: target, text: pending})
	}
	return lines
}

func containsToolingIdentifier(contents string, identifier string) bool {
	for offset := 0; ; {
		index := strings.Index(contents[offset:], identifier)
		if index < 0 {
			return false
		}
		start, end := offset+index, offset+index+len(identifier)
		if (start == 0 || !isIdentifierByte(contents[start-1])) && (end == len(contents) || !isIdentifierByte(contents[end])) {
			return true
		}
		offset = start + 1
	}
}

func isIdentifierByte(character byte) bool {
	return character == '_' || character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func TestConnectorTestSelectionIncludesTransitiveDependents(t *testing.T) {
	repositoryRoot := t.TempDir()
	directories := []string{"connectors/acme/base", "connectors/acme/first", "connectors/acme/second", "connectors/acme/unrelated"}
	for _, directory := range directories {
		moduleDirectory := filepath.Join(repositoryRoot, directory, "go")
		require.NoError(t, os.MkdirAll(moduleDirectory, 0o755))
		module := "module github.com/superdurable/dex-connectors-library/" + directory + "/go\n"
		switch directory {
		case "connectors/acme/first":
			module += "require github.com/superdurable/dex-connectors-library/connectors/acme/base/go v0.1.0\n"
		case "connectors/acme/second":
			module += "require github.com/superdurable/dex-connectors-library/connectors/acme/first/go v0.1.0\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.mod"), []byte(module), 0o644))
	}
	selected, err := includeConnectorDependents(repositoryRoot, directories, directories[:1])
	require.NoError(t, err)
	require.Equal(t, directories[:3], selected)
}
