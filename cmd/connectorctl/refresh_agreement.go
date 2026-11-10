// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

// generatedRefreshAgreementFiles returns the credential refresh agreement test of a manifest that declares token
// grants, keyed by path, or no files when it declares none.
func generatedRefreshAgreementFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	if !codegen.HasDeclaredTokenGrants(manifest) {
		return map[string][]byte{}, nil
	}
	directory := filepath.Join(filepath.Dir(manifestPath), "go")
	modulePath, err := readModulePath(filepath.Join(directory, "go.mod"))
	if err != nil {
		return nil, err
	}
	samples, err := readRefreshAgreementSamples(filepath.Join(directory, "testdata", codegen.RefreshAgreementSamplesFile))
	if err != nil {
		return nil, err
	}
	test, err := codegen.GenerateRefreshAgreementTest(manifest, modulePath, samples)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	files := map[string][]byte{filepath.Join(directory, codegen.RefreshAgreementTestOutputFile): test}
	transportHook, err := codegen.GenerateRefreshAgreementTransportHookTest(manifest.Spec.Codegen.Go.Package)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	files[filepath.Join(directory, codegen.RefreshAgreementTransportHookOutputFile)] = transportHook
	cases, err := codegen.GenerateRefreshAgreementCases(manifest, samples)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	connectorDirectory := filepath.Dir(manifestPath)
	if manifest.Spec.Languages != nil && manifest.Spec.Languages.Python != nil {
		pythonTests := filepath.Join(connectorDirectory, "python", "tests")
		pythonCases, err := languageRefreshAgreementCases(cases, filepath.Join(connectorDirectory, "python", "pyproject.toml"), "python")
		if err != nil {
			return nil, err
		}
		files[filepath.Join(pythonTests, "refresh_agreement_cases.json")] = pythonCases
		relativeDirectory, err := connectorDirectoryInCatalog(manifestPath)
		if err != nil {
			return nil, err
		}
		files[filepath.Join(pythonTests, "test_refresh_agreement.py")] = codegen.GeneratePythonRefreshAgreementTest(pythonImportPackageName(relativeDirectory))
	}
	if manifest.Spec.Languages != nil && manifest.Spec.Languages.TypeScript != nil {
		typeScriptTests := filepath.Join(connectorDirectory, "typescript", "test")
		typeScriptCases, err := languageRefreshAgreementCases(cases, filepath.Join(connectorDirectory, "typescript", "package.json"), "typescript")
		if err != nil {
			return nil, err
		}
		files[filepath.Join(typeScriptTests, "fixtures", "refresh-agreement-cases.json")] = typeScriptCases
		files[filepath.Join(typeScriptTests, "refresh_agreement.test.ts")] = codegen.GenerateTypeScriptRefreshAgreementTest()
	}
	return files, nil
}

// pythonConnectorSDKRequirement matches a legacy exact pin or a version floor below 1; the floor is the lowest SDK
// release a package supports, so it decides which SDK features the generated cases may assume.
var pythonConnectorSDKRequirement = regexp.MustCompile(`["']dex-connectors-sdk(?:==([0-9]+\.[0-9]+\.[0-9]+)|>=(0\.[0-9]+\.[0-9]+),<1)["']`)

// languageRefreshAgreementCases checks the package's SDK requirement before emitting timeout enforcement cases.
func languageRefreshAgreementCases(cases []byte, packagePath, language string) ([]byte, error) {
	contents, err := os.ReadFile(packagePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", packagePath, err)
	}
	var version string
	switch language {
	case "python":
		requirements := pythonConnectorSDKRequirement.FindAllSubmatch(contents, -1)
		if len(requirements) != 1 {
			return nil, fmt.Errorf("%s must require dex-connectors-sdk once as >=X.Y.Z,<1 or a legacy ==X.Y.Z", packagePath)
		}
		version = string(requirements[0][1]) + string(requirements[0][2])
	case "typescript":
		var declaration struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if err := json.Unmarshal(contents, &declaration); err != nil {
			return nil, fmt.Errorf("decode %s: %w", packagePath, err)
		}
		version = declaration.Dependencies["@superdurable/connectors-sdk"]
	default:
		return nil, fmt.Errorf("unsupported language %s", language)
	}
	versionParts := regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`).FindStringSubmatch(version)
	if versionParts == nil {
		return nil, fmt.Errorf("%s must pin Connector SDK to an exact version: %q", packagePath, version)
	}
	major, _ := strconv.Atoi(versionParts[1])
	minor, _ := strconv.Atoi(versionParts[2])
	if major > 0 || minor >= 8 {
		return cases, nil
	}
	var agreements []codegen.RefreshAgreementCase
	if err := json.Unmarshal(cases, &agreements); err != nil {
		return nil, fmt.Errorf("decode refresh agreement cases: %w", err)
	}
	for index := range agreements {
		agreements[index].TokenExchangeTimeout = ""
		agreements[index].TokenExchangeTimeoutNanoseconds = nil
	}
	encoded, err := json.MarshalIndent(agreements, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode refresh agreement cases: %w", err)
	}
	return append(encoded, '\n'), nil
}

// staleRefreshAgreementFiles lists the agreement test that a manifest without token grants must not keep.
func staleRefreshAgreementFiles(manifestPath string, manifest schema.Manifest) []string {
	if codegen.HasDeclaredTokenGrants(manifest) {
		return nil
	}
	var stale []string
	for _, file := range []string{codegen.RefreshAgreementTestOutputFile, codegen.RefreshAgreementTransportHookOutputFile} {
		path := filepath.Join(filepath.Dir(manifestPath), "go", file)
		if _, err := os.Lstat(path); err == nil {
			stale = append(stale, path)
		}
	}
	return stale
}

// readRefreshAgreementSamples reads the optional sample overrides strictly; a missing file means none.
func readRefreshAgreementSamples(path string) (map[string]codegen.RefreshAgreementSample, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var samples map[string]codegen.RefreshAgreementSample
	if err := decoder.Decode(&samples); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return samples, nil
}
