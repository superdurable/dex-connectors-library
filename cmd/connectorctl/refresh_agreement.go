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
	return map[string][]byte{filepath.Join(directory, codegen.RefreshAgreementTestOutputFile): test}, nil
}

// staleRefreshAgreementFiles lists the agreement test that a manifest without token grants must not keep.
func staleRefreshAgreementFiles(manifestPath string, manifest schema.Manifest) []string {
	path := filepath.Join(filepath.Dir(manifestPath), "go", codegen.RefreshAgreementTestOutputFile)
	if _, err := os.Lstat(path); err != nil || codegen.HasDeclaredTokenGrants(manifest) {
		return nil
	}
	return []string{path}
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
