// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
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

// hasPythonRepeatOnUnknownSendCaps requires the [project].dependencies of python/pyproject.toml to admit no
// dex-connectors-sdk release before v0.10.0. Optional dependencies and dependency groups never count, and a
// missing package, a marker, or any requirement form pythonConnectorSDKFloor does not recognize keeps the older output.
func hasPythonRepeatOnUnknownSendCaps(manifestPath string) (bool, error) {
	pyprojectPath := filepath.Join(filepath.Dir(manifestPath), "python", "pyproject.toml")
	contents, err := readOptionalLanguagePackageFile(pyprojectPath)
	if err != nil || contents == nil {
		return false, err
	}
	var declaration struct {
		Project struct {
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
	}
	if _, err := toml.Decode(string(contents), &declaration); err != nil {
		return false, fmt.Errorf("parse %s: %w", pyprojectPath, err)
	}
	var floors [][]int
	for _, requirement := range declaration.Project.Dependencies {
		name, specifiers, isRecognized := splitPythonRequirement(requirement)
		if name != pythonConnectorSDKDistribution {
			continue
		}
		floor, hasFloor := pythonConnectorSDKFloor(specifiers)
		if !isRecognized || !hasFloor {
			return false, nil
		}
		floors = append(floors, floor)
	}
	minimum, _ := pythonReleaseSegments(strings.TrimPrefix(codegen.PythonRepeatOnUnknownMinimumSDKVersion, "v"))
	return len(floors) == 1 && comparePythonReleases(floors[0], minimum) >= 0, nil
}

const pythonConnectorSDKDistribution = "dex-connectors-sdk"

var (
	// pythonRequirementPattern splits a PEP 508 name, optional extras, specifiers, and an optional marker.
	pythonRequirementPattern = regexp.MustCompile(`^\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(\[[^\]]*\])?\s*([^;]*?)\s*(;.*)?$`)
	pythonSpecifierPattern   = regexp.MustCompile(`^\s*(==|>=|>|<=|<|!=|~=)\s*([0-9]+(?:\.[0-9]+)*)\s*$`)
)

// splitPythonRequirement returns the PEP 503 name and specifiers; a marker or an unparsable requirement is unrecognized.
func splitPythonRequirement(requirement string) (name, specifiers string, isRecognized bool) {
	parts := pythonRequirementPattern.FindStringSubmatch(requirement)
	if parts == nil {
		return "", "", false
	}
	name = pythonNameSeparators.ReplaceAllString(strings.ToLower(parts[1]), "-")
	return name, parts[3], parts[4] == ""
}

// pythonConnectorSDKFloor returns the lowest release the PEP 440 specifiers admit: the largest of their
// ==, >=, ~=, and > versions, where > counts its version as a conservative floor. Upper bounds and
// exclusions cannot lower it. Wildcards, pre-releases, local versions, and === are not recognized.
func pythonConnectorSDKFloor(specifiers string) ([]int, bool) {
	var floor []int
	for _, clause := range strings.Split(specifiers, ",") {
		parts := pythonSpecifierPattern.FindStringSubmatch(clause)
		if parts == nil {
			return nil, false
		}
		version, _ := pythonReleaseSegments(parts[2])
		switch parts[1] {
		case "==", ">=", "~=", ">":
			if floor == nil || comparePythonReleases(version, floor) > 0 {
				floor = version
			}
		}
	}
	return floor, floor != nil
}

func pythonReleaseSegments(version string) ([]int, bool) {
	var segments []int
	for _, segment := range strings.Split(version, ".") {
		value, err := strconv.Atoi(segment)
		if err != nil {
			return nil, false
		}
		segments = append(segments, value)
	}
	return segments, true
}

// comparePythonReleases compares PEP 440 release segments, padding the shorter one with zeros.
func comparePythonReleases(left, right []int) int {
	for index := 0; index < max(len(left), len(right)); index++ {
		var leftSegment, rightSegment int
		if index < len(left) {
			leftSegment = left[index]
		}
		if index < len(right) {
			rightSegment = right[index]
		}
		if leftSegment != rightSegment {
			return leftSegment - rightSegment
		}
	}
	return 0
}

// hasTypeScriptRepeatOnUnknownSendCaps requires an exact @superdurable/connectors-sdk dependency of at least
// v0.11.0 in typescript/package.json. A missing package or a range keeps the older output.
func hasTypeScriptRepeatOnUnknownSendCaps(manifestPath string) (bool, error) {
	packagePath := filepath.Join(filepath.Dir(manifestPath), "typescript", "package.json")
	contents, err := readOptionalLanguagePackageFile(packagePath)
	if err != nil || contents == nil {
		return false, err
	}
	var declaration struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(contents, &declaration); err != nil {
		return false, fmt.Errorf("decode %s: %w", packagePath, err)
	}
	version := "v" + declaration.Dependencies["@superdurable/connectors-sdk"]
	if !exactReleaseVersionPattern.MatchString(version) {
		return false, nil
	}
	return semver.Compare(version, codegen.TypeScriptRepeatOnUnknownMinimumSDKVersion) >= 0, nil
}

var exactReleaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// readOptionalLanguagePackageFile returns nil contents for a language package that does not exist.
func readOptionalLanguagePackageFile(path string) ([]byte, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return contents, nil
}
