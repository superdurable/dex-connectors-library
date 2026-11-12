// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConnectorREADMEsNameTheManifestVersion keeps every version that a connector README pins for its own module on
// the manifest version: the go get commands and the raw tag URLs of its examples, the release-artifact command's
// --version and --tag, and the "Connector `vX.Y.Z`" and "Before `vX.Y.Z` is published" sentences. A version of
// another module, the SDK, or Dex is not matched. connectorctl bump rewrites the same pins, and also the exact
// Python "<package>==X.Y.Z" and TypeScript "<package>@X.Y.Z" pins, which stay on each declared package version.
func TestConnectorREADMEsNameTheManifestVersion(t *testing.T) {
	var stalePins []string
	for _, directory := range registeredConnectorDirectories(t) {
		stalePins = append(stalePins, staleREADMEVersionPins(t, filepath.Join("..", "..", filepath.FromSlash(directory)), directory)...)
	}
	require.Empty(t, stalePins)
}

// staleREADMEVersionPins describes each README pin that differs from its package's declared version.
func staleREADMEVersionPins(t *testing.T, connectorPath, directory string) []string {
	t.Helper()
	manifest, err := load(filepath.Join(connectorPath, "connector.yaml"))
	require.NoError(t, err)
	type packagePins struct{ language, packageName, version string }
	packages := []packagePins{{language: "go", version: manifest.Metadata.Version}}
	if languages := manifest.Spec.Languages; languages != nil {
		if languages.Python != nil {
			packages = append(packages, packagePins{"python", languagePackageName(directory, "python", languages.Python.Package), strings.TrimPrefix(languages.Python.Version, "v")})
		}
		if languages.TypeScript != nil {
			packages = append(packages, packagePins{"typescript", languagePackageName(directory, "typescript", languages.TypeScript.Package), strings.TrimPrefix(languages.TypeScript.Version, "v")})
		}
	}
	readmePaths, nestedConnectorDirectories, err := connectorREADMEPaths(connectorPath)
	require.NoError(t, err)
	var stalePins []string
	for _, path := range readmePaths {
		contents, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, declared := range packages {
			for _, pin := range findREADMEVersionPins(path, contents, directory, nestedConnectorDirectories, declared.language, declared.packageName) {
				if pin.version != declared.version {
					stalePins = append(stalePins, fmt.Sprintf("%s pins %q, not %s", strings.TrimPrefix(path, "../../"), pin.text, declared.version))
				}
			}
		}
	}
	return stalePins
}
