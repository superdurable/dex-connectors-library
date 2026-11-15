// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// repeatOnUnknownGenerateOptions emits send caps only for a module whose sdkgo requirement defines them.
func repeatOnUnknownGenerateOptions(manifestPath string) ([]codegen.GenerateOption, error) {
	hasSendCaps, err := hasRepeatOnUnknownSendCaps(manifestPath)
	if err != nil || !hasSendCaps {
		return nil, err
	}
	return []codegen.GenerateOption{codegen.WithRepeatOnUnknownSendCaps()}, nil
}

// hasRepeatOnUnknownSendCaps requires a released sdkgo of at least v0.37.0 in go/go.mod.
// A missing module, a replace, or an unreleased version keeps the older output, which every sdkgo compiles.
func hasRepeatOnUnknownSendCaps(manifestPath string) (bool, error) {
	goModulePath := filepath.Join(filepath.Dir(manifestPath), "go", "go.mod")
	contents, err := os.ReadFile(goModulePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read connector module %s: %w", goModulePath, err)
	}
	module, err := modfile.Parse(goModulePath, contents, nil)
	if err != nil {
		return false, fmt.Errorf("parse connector module %s: %w", goModulePath, err)
	}
	for _, replacement := range module.Replace {
		if replacement.Old.Path == connectorSDKModulePath {
			return false, nil
		}
	}
	for _, requirement := range module.Require {
		if requirement.Mod.Path != connectorSDKModulePath {
			continue
		}
		version := requirement.Mod.Version
		if !semver.IsValid(version) || semver.Prerelease(version) != "" || semver.Build(version) != "" {
			return false, nil
		}
		return semver.Compare(version, codegen.RepeatOnUnknownMinimumSDKVersion) >= 0, nil
	}
	return false, nil
}
