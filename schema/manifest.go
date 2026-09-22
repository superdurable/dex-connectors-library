// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package schema loads and validates connector manifests.
package schema

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const APIVersion = "connectors.dex.dev/v1alpha1"

type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"displayName" json:"displayName"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
}

type Spec struct {
	Provider   string      `yaml:"provider" json:"provider"`
	Auth       Auth        `yaml:"auth" json:"auth"`
	Operations []Operation `yaml:"operations" json:"operations"`
}

type Auth struct {
	Type   string      `yaml:"type" json:"type"`
	Fields []AuthField `yaml:"fields" json:"fields"`
}

type AuthField struct {
	Name      string `yaml:"name" json:"name"`
	Required  bool   `yaml:"required" json:"required"`
	Sensitive bool   `yaml:"sensitive" json:"sensitive"`
}

type Operation struct {
	Name        string   `yaml:"name" json:"name"`
	Kind        string   `yaml:"kind" json:"kind"`
	Description string   `yaml:"description" json:"description"`
	Idempotency string   `yaml:"idempotency" json:"idempotency"`
	Progress    []string `yaml:"progress,omitempty" json:"progress,omitempty"`
}

var (
	namePattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	operationPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)
	versionPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([+-].*)?$`)
)

func Decode(reader io.Reader) (Manifest, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	for index := range manifest.Spec.Operations {
		sort.Strings(manifest.Spec.Operations[index].Progress)
	}
	return manifest, nil
}

func (manifest Manifest) Validate() error {
	var problems []string
	if manifest.APIVersion != APIVersion {
		problems = append(problems, "apiVersion must be "+APIVersion)
	}
	if manifest.Kind != "Connector" {
		problems = append(problems, "kind must be Connector")
	}
	if !namePattern.MatchString(manifest.Metadata.Name) {
		problems = append(problems, "metadata.name must be DNS-like and 2-63 characters")
	}
	if strings.TrimSpace(manifest.Metadata.DisplayName) == "" || strings.TrimSpace(manifest.Metadata.Description) == "" {
		problems = append(problems, "metadata.displayName and metadata.description are required")
	}
	if !versionPattern.MatchString(manifest.Metadata.Version) {
		problems = append(problems, "metadata.version must be a v-prefixed semantic version")
	}
	if strings.TrimSpace(manifest.Spec.Provider) == "" {
		problems = append(problems, "spec.provider is required")
	}
	if manifest.Spec.Auth.Type != "none" && manifest.Spec.Auth.Type != "apiKey" && manifest.Spec.Auth.Type != "oauth2" {
		problems = append(problems, "spec.auth.type must be none, apiKey, or oauth2")
	}
	seenFields := map[string]bool{}
	for _, field := range manifest.Spec.Auth.Fields {
		if field.Name == "" || seenFields[field.Name] {
			problems = append(problems, "auth field names must be non-empty and unique")
		}
		seenFields[field.Name] = true
	}
	if len(manifest.Spec.Operations) == 0 {
		problems = append(problems, "spec.operations must contain at least one operation")
	}
	seenOperations := map[string]bool{}
	for _, operation := range manifest.Spec.Operations {
		if !operationPattern.MatchString(operation.Name) || seenOperations[operation.Name] {
			problems = append(problems, "operation names must be lower camel case and unique")
		}
		seenOperations[operation.Name] = true
		if operation.Kind != "query" && operation.Kind != "mutation" {
			problems = append(problems, operation.Name+": kind must be query or mutation")
		}
		if operation.Idempotency != "none" && operation.Idempotency != "required" {
			problems = append(problems, operation.Name+": invalid idempotency")
		}
		if operation.Kind == "mutation" && operation.Idempotency == "none" {
			problems = append(problems, operation.Name+": mutations must declare required idempotency")
		}
		seenProgress := map[string]bool{}
		for _, capability := range operation.Progress {
			if capability != "structured" && capability != "text" {
				problems = append(problems, operation.Name+": progress must contain only structured or text")
			}
			if seenProgress[capability] {
				problems = append(problems, operation.Name+": progress capabilities must be unique")
			}
			seenProgress[capability] = true
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("invalid connector manifest: %s", strings.Join(problems, "; "))
	}
	return nil
}
