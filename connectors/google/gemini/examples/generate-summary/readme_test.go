// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestREADMECommandsRunFromTheirWorkingDirectory resolves README paths after each cd and requires the dexcli dev release override.
func TestREADMECommandsRunFromTheirWorkingDirectory(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	repositoryRoot := filepath.Dir(filepath.Dir(filepath.Dir(moduleRoot)))
	require.FileExists(t, filepath.Join(repositoryRoot, "connectors.yaml"))
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)

	// The first block runs from connectors/google/gemini, as the README says.
	directory := moduleRoot
	renderingDirectory := ""
	dexDevCommands, workerCommands := 0, 0
	for _, command := range readmeShellCommands(string(readme)) {
		fields := strings.Fields(command)
		for len(fields) > 0 && strings.Contains(fields[0], "=") {
			fields = fields[1:]
		}
		if len(fields) < 2 {
			continue
		}
		switch {
		case fields[0] == "cd":
			path := strings.Trim(strings.Join(fields[1:], " "), `"`)
			path = strings.ReplaceAll(path, "$(git rev-parse --show-toplevel)", repositoryRoot)
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			directory = filepath.Clean(path)
			require.DirExists(t, directory, command)
		case fields[0] == "go" && (fields[1] == "run" || fields[1] == "test"):
			for _, argument := range fields[2:] {
				if strings.HasPrefix(argument, "./") {
					require.DirExists(t, filepath.Join(directory, strings.TrimSuffix(argument, "/...")), "%q runs from %s", command, directory)
					if fields[1] == "run" && argument == "./examples/generate-summary" {
						workerCommands++
					}
					break
				}
			}
			if manifest := flagValue(fields, "--manifest"); manifest != "" {
				require.FileExists(t, filepath.Join(directory, manifest), command)
			}
		case fields[0] == "dexcli" && fields[1] == "visualize":
			require.FileExists(t, filepath.Join(directory, fields[2]), command)
			renderingDirectory = filepath.Dir(filepath.Join(directory, flagValue(fields, "--out")))
		case fields[0] == "dexcli" && fields[1] == "dev":
			dexDevCommands++
			require.NotEmpty(t, flagValue(fields, "--connector-release-override"),
				"the connection of an in-repository example is Unsupported without a local release override: %s", command)
			flowRenderingDirectory := strings.ReplaceAll(strings.Trim(flagValue(fields, "--flow-rendering-dir"), `"`), "$PWD", directory)
			require.Equal(t, renderingDirectory, filepath.Clean(flowRenderingDirectory), "dexcli dev must read the definition that dexcli visualize wrote")
		}
	}
	require.Equal(t, 1, dexDevCommands)
	require.Equal(t, 1, workerCommands)
}

// readmeShellCommands returns the commands of every bash block, joining backslash continuations.
func readmeShellCommands(readme string) []string {
	var commands []string
	isInBashBlock := false
	pending := ""
	for _, line := range strings.Split(readme, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !isInBashBlock && trimmed == "```bash":
			isInBashBlock = true
		case isInBashBlock && trimmed == "```":
			isInBashBlock = false
		case isInBashBlock:
			if strings.HasSuffix(trimmed, "\\") {
				pending += strings.TrimSuffix(trimmed, "\\") + " "
				continue
			}
			commands = append(commands, pending+trimmed)
			pending = ""
		}
	}
	return commands
}

func flagValue(fields []string, name string) string {
	for index, field := range fields {
		if field == name && index+1 < len(fields) {
			return fields[index+1]
		}
		if value, ok := strings.CutPrefix(field, name+"="); ok {
			return value
		}
	}
	return ""
}
