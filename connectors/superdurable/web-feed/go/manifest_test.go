// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type   string          `yaml:"type"`
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"auth"`
		Operations []struct {
			Name          string `yaml:"name"`
			Kind          string `yaml:"kind"`
			Authorization string `yaml:"authorization"`
			Execution     struct {
				Durability    string `yaml:"durability"`
				DispatchFence bool   `yaml:"dispatchFence"`
				Retry         any    `yaml:"retry"`
			} `yaml:"execution"`
			Branches []struct {
				ID       string `yaml:"id"`
				Optional bool   `yaml:"optional"`
			} `yaml:"branches"`
			Agent struct {
				Exposed     bool              `yaml:"exposed"`
				Approval    string            `yaml:"approval"`
				Retry       string            `yaml:"retry"`
				Branches    map[string]string `yaml:"branches"`
				Description string            `yaml:"description"`
			} `yaml:"agent"`
		} `yaml:"operations"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "none", manifest.Spec.Auth.Type, "public reads need no credential")
	require.Empty(t, manifest.Spec.Auth.Fields)
	var names []string
	for _, field := range manifest.Spec.Configuration.Fields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"timeout", "maxDocumentBytes", "maxPageBytes", "userAgent"}, names, "a new visible field needs its own guidance review")
	for _, field := range manifest.Spec.Configuration.Fields {
		t.Run(field.Name, func(t *testing.T) {
			require.False(t, field.Required, "every setting has a safe default")
			require.NotNil(t, field.Default)
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			require.Contains(t, field.Description, "Blank uses the displayed default", "every field explains what blank means")
			require.NotEqual(t, "secretString", field.Type)
		})
	}
	require.Nil(t, manifest.Spec.Studio, "the connector ships no Studio bundle; URLs come from Flow input")
}

func TestOperationsAreRetrySafeReadsWithOneHappyPath(t *testing.T) {
	operations := readGuidanceManifest(t).Spec.Operations
	expected := map[string][]string{
		"readFeed":         {"read", "notModified", "notFound", "providerRejected", "unreachable", "invalidResponse", "defect"},
		"discoverFeeds":    {"discovered", "notFound", "providerRejected", "unreachable", "invalidResponse", "defect"},
		"readSitemap":      {"read", "notModified", "notFound", "providerRejected", "unreachable", "invalidResponse", "defect"},
		"readPageMetadata": {"read", "notFound", "providerRejected", "unreachable", "invalidResponse", "defect"},
	}
	require.Len(t, operations, len(expected))
	for _, operation := range operations {
		t.Run(operation.Name, func(t *testing.T) {
			require.Equal(t, "query", operation.Kind)
			require.Equal(t, "none", operation.Authorization)
			require.Equal(t, "async", operation.Execution.Durability)
			require.False(t, operation.Execution.DispatchFence)
			require.Nil(t, operation.Execution.Retry, "reads use the default one-hour Connector Step retry policy")
			require.True(t, operation.Agent.Exposed, "every public read is offered to agents")
			require.Empty(t, operation.Agent.Approval, "a public read needs no approval")
			require.Empty(t, operation.Agent.Retry, "retry claims apply to Mutations only")
			require.GreaterOrEqual(t, len(operation.Agent.Description), 200, "the model needs inputs, outputs, and the next tool")
			require.NotContains(t, operation.Agent.Description, "retry")
			var optionalBranches []string
			for _, branch := range operation.Branches[1:] {
				optionalBranches = append(optionalBranches, branch.ID)
				require.Equal(t, "rejected", operation.Agent.Branches[branch.ID], "a read changes nothing, so every optional branch is rejected")
			}
			require.Len(t, operation.Agent.Branches, len(optionalBranches))
			var identifiers []string
			for index, branch := range operation.Branches {
				identifiers = append(identifiers, branch.ID)
				require.Equal(t, index > 0, branch.Optional, "only the first branch is the happy path")
			}
			require.Equal(t, expected[operation.Name], identifiers)
		})
	}
}

func TestREADMENamesTheManifestDefaults(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	defaults := webfeed.DefaultConfig()
	require.Contains(t, string(readme), "| `timeout` | `"+defaults.Timeout.String()+"` |")
	require.Contains(t, string(readme), "| `maxDocumentBytes` | `"+strconv.FormatInt(defaults.MaxDocumentBytes, 10)+"` |")
	require.Contains(t, string(readme), "| `maxPageBytes` | `"+strconv.FormatInt(defaults.MaxPageBytes, 10)+"` |")
	require.Contains(t, string(readme), "| `userAgent` | `"+defaults.UserAgent+"` |")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}
