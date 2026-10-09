// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

// generatedMockFiles returns the mock package files for a manifest beside its go.mod, keyed by path, or no files
// when the manifest declares no mocks.
func generatedMockFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	if !manifest.HasMocks() {
		return map[string][]byte{}, nil
	}
	modulePath, err := readModulePath(filepath.Join(filepath.Dir(manifestPath), "go.mod"))
	if err != nil {
		return nil, err
	}
	source, test, err := codegen.GenerateMockPackage(manifest, modulePath)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(filepath.Dir(manifestPath), codegen.MockPackageName(manifest))
	return map[string][]byte{
		filepath.Join(directory, codegen.MockOutputFile):     source,
		filepath.Join(directory, codegen.MockTestOutputFile): test,
	}, nil
}

// staleMockFiles lists the generated mock files that a manifest without mocks must not keep.
func staleMockFiles(manifestPath string, manifest schema.Manifest) []string {
	if manifest.HasMocks() {
		return nil
	}
	directory := filepath.Join(filepath.Dir(manifestPath), codegen.MockPackageName(manifest))
	var stale []string
	for _, name := range []string{codegen.MockOutputFile, codegen.MockTestOutputFile} {
		if _, err := os.Lstat(filepath.Join(directory, name)); err == nil {
			stale = append(stale, filepath.Join(directory, name))
		}
	}
	return stale
}

func mocksCommand(args []string, stderr io.Writer) error {
	if len(args) != 2 || args[0] != "scaffold" {
		return errors.New("usage: connectorctl mocks scaffold <manifest>")
	}
	return scaffoldMocks(args[1], stderr)
}

// scaffoldMocks appends a failure fixture for every declared failure branch that has no mock, and an uncertain
// fixture for a mutation's uncertain branch. It edits the manifest text in place, so the rest of the file keeps
// its formatting, and reports the branches whose outputs a maintainer still writes by hand.
func scaffoldMocks(manifestPath string, stderr io.Writer) error {
	manifest, err := load(manifestPath)
	if err != nil {
		return err
	}
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	insertions, err := mockInsertionPoints(contents)
	if err != nil {
		return fmt.Errorf("%s: %w", manifestPath, err)
	}
	lines := strings.SplitAfter(string(contents), "\n")
	for index := len(manifest.Spec.Operations) - 1; index >= 0; index-- {
		operation := manifest.Spec.Operations[index]
		insertion := insertions[index]
		scaffolded := scaffoldOperationMocks(operation, insertion, stderr)
		if scaffolded == "" {
			continue
		}
		lines = append(lines[:insertion.line], append([]string{scaffolded}, lines[insertion.line:]...)...)
	}
	updated := strings.Join(lines, "")
	if _, err := schema.Decode(strings.NewReader(updated)); err != nil {
		return fmt.Errorf("scaffolded manifest is invalid: %w", err)
	}
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", manifestPath, err)
	}
	return nil
}

// mockInsertion is where one operation's scaffolded mocks go: after line (zero-based, exclusive), indented as
// itemIndent, with hasMocksKey reporting that the operation already ends with a mocks sequence.
type mockInsertion struct {
	line        int
	keyIndent   int
	hasMocksKey bool
}

func mockInsertionPoints(contents []byte) ([]mockInsertion, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, err
	}
	operations := mappingValue(mappingValue(document.Content[0], "spec"), "operations")
	if operations == nil || operations.Kind != yaml.SequenceNode {
		return nil, errors.New("spec.operations is not a block sequence")
	}
	lines := strings.Split(string(contents), "\n")
	insertions := make([]mockInsertion, len(operations.Content))
	for index, operation := range operations.Content {
		if operation.Kind != yaml.MappingNode || operation.Style&yaml.FlowStyle != 0 {
			return nil, fmt.Errorf("operation %d is not a block mapping", index+1)
		}
		end := len(lines)
		if index+1 < len(operations.Content) {
			end = operations.Content[index+1].Line - 1
		} else if next := nextSpecKeyLine(document.Content[0]); next > 0 {
			end = next - 1
		}
		for end > 0 && isBlankOrComment(lines[end-1]) {
			end--
		}
		lastKey := operation.Content[len(operation.Content)-2]
		hasMocksKey := mappingValue(operation, "mocks") != nil
		if hasMocksKey && lastKey.Value != "mocks" {
			return nil, fmt.Errorf("operation %d: move mocks to the end of the operation before scaffolding", index+1)
		}
		insertions[index] = mockInsertion{line: end, keyIndent: operation.Content[0].Column - 1, hasMocksKey: hasMocksKey}
	}
	return insertions, nil
}

func scaffoldOperationMocks(operation schema.Operation, insertion mockInsertion, stderr io.Writer) string {
	covered := map[string]bool{}
	names := map[string]bool{}
	for _, mock := range operation.Mocks {
		names[mock.Name] = true
		if mock.Uncertain != nil {
			covered["uncertain"] = true
		} else if mock.IsBranchMock() {
			covered[mock.Branch] = true
		}
	}
	var output strings.Builder
	keyIndent := strings.Repeat(" ", insertion.keyIndent)
	if !insertion.hasMocksKey {
		output.WriteString(keyIndent + "mocks:\n")
	}
	itemIndent := keyIndent + "  "
	scaffolded := 0
	for _, branch := range operation.Branches {
		if covered[branch.ID] {
			continue
		}
		if !branch.Optional {
			// The note is advisory; a failed write to stderr must not stop the scaffold.
			_, _ = fmt.Fprintf(stderr, "%s: write the %s mock output by hand, and mark one query mock as the default\n", operation.Name, branch.ID)
			continue
		}
		name := uniqueMockName(names, branch.ID+"Failure")
		message := strings.TrimSpace(branch.Description)
		if branch.ID == "uncertain" {
			name = uniqueMockName(names, "lostResponse")
			output.WriteString(itemIndent + "- name: " + name + "\n")
			output.WriteString(itemIndent + "  description: " + yamlString("The connection closed after the request was sent, so the outcome is unknown.") + "\n")
			output.WriteString(itemIndent + "  uncertain: {failure: {kind: TRANSPORT, message: " + yamlString(message) + "}}\n")
		} else {
			output.WriteString(itemIndent + "- name: " + name + "\n")
			output.WriteString(itemIndent + "  branch: " + branch.ID + "\n")
			output.WriteString(itemIndent + "  description: " + yamlString(message) + "\n")
			output.WriteString(itemIndent + "  failure: {kind: " + scaffoldFailureKind(branch.ID) + ", message: " + yamlString(message) + "}\n")
		}
		scaffolded++
	}
	if scaffolded == 0 {
		return ""
	}
	return output.String()
}

// scaffoldFailureKind guesses the failure kind from the branch vocabulary the connectors share.
func scaffoldFailureKind(branchID string) string {
	switch branchID {
	case "defect":
		return "VALIDATION"
	case "notFound":
		return "NOT_FOUND"
	case "insufficientScope", "forbidden", "permissionDenied":
		return "AUTHORIZATION"
	case "authorizationRevoked", "unauthorized", "authenticationFailed":
		return "AUTHENTICATION"
	case "rateLimited":
		return "RATE_LIMIT"
	case "conflict", "alreadyExists":
		return "CONFLICT"
	case "invalidResponse":
		return "PROTOCOL"
	case "quotaExhausted":
		return "QUOTA_EXHAUSTED"
	default:
		return "PROVIDER_REJECTION"
	}
}

func uniqueMockName(names map[string]bool, candidate string) string {
	name := candidate
	for suffix := 2; names[name]; suffix++ {
		name = fmt.Sprintf("%s%d", candidate, suffix)
	}
	names[name] = true
	return name
}

// yamlString returns a double-quoted scalar; a JSON string is a valid YAML double-quoted scalar.
func yamlString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode a Go string as JSON: %v", err))
	}
	return string(encoded)
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// nextSpecKeyLine returns the line of the key that follows spec.operations, or of the top-level key that follows
// spec, or zero when operations end the file.
func nextSpecKeyLine(root *yaml.Node) int {
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value != "spec" {
			continue
		}
		spec := root.Content[index+1]
		for keyIndex := 0; keyIndex+1 < len(spec.Content); keyIndex += 2 {
			if spec.Content[keyIndex].Value == "operations" && keyIndex+2 < len(spec.Content) {
				return spec.Content[keyIndex+2].Line
			}
		}
		if index+2 < len(root.Content) {
			return root.Content[index+2].Line
		}
	}
	return 0
}

func isBlankOrComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

func writeGeneratedFiles(files map[string][]byte) error {
	for path, contents := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func checkGeneratedFiles(files map[string][]byte) error {
	for path, generated := range files {
		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("generated file is missing: %s", path)
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("generated file is stale: %s", path)
		}
	}
	return nil
}
