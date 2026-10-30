// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

// generatedSharedCredentialFiles returns the shared credential test of a manifest whose host keeps credentials,
// keyed by path, or no files when the host keeps none.
func generatedSharedCredentialFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	if !codegen.HasHostKeptCredentials(manifest) {
		return map[string][]byte{}, nil
	}
	directory := filepath.Join(filepath.Dir(manifestPath), "go")
	modulePath, err := readModulePath(filepath.Join(directory, "go.mod"))
	if err != nil {
		return nil, err
	}
	test, err := codegen.GenerateSharedCredentialTest(manifest, modulePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return map[string][]byte{filepath.Join(directory, codegen.SharedCredentialTestOutputFile): test}, nil
}

// staleSharedCredentialFiles lists the shared credential test that a manifest without host-kept credentials must
// not keep.
func staleSharedCredentialFiles(manifestPath string, manifest schema.Manifest) []string {
	path := filepath.Join(filepath.Dir(manifestPath), "go", codegen.SharedCredentialTestOutputFile)
	if _, err := os.Lstat(path); err != nil || codegen.HasHostKeptCredentials(manifest) {
		return nil
	}
	return []string{path}
}
