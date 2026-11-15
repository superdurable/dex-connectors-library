// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendCapsFollowTheRequiredSDKRelease(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		goModule    string
		hasSendCaps bool
	}{
		{name: "earlier release", goModule: "require " + connectorSDKModulePath + " v0.36.0\n"},
		{name: "first release with send caps", goModule: "require " + connectorSDKModulePath + " v0.37.0\n", hasSendCaps: true},
		{name: "later release", goModule: "require " + connectorSDKModulePath + " v0.38.2\n", hasSendCaps: true},
		{name: "prerelease", goModule: "require " + connectorSDKModulePath + " v0.37.0-rc.1\n"},
		{name: "pseudo-version", goModule: "require " + connectorSDKModulePath + " v0.37.1-0.20261114000000-abcdefabcdef\n"},
		{name: "replaced", goModule: "require " + connectorSDKModulePath + " v0.37.0\n\nreplace " + connectorSDKModulePath + " => ../../sdkgo\n"},
		{name: "no requirement", goModule: ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(directory, "go"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "go", "go.mod"), []byte("module example.com/connector\n\ngo 1.24.0\n\n"+scenario.goModule), 0o600))
			hasSendCaps, err := hasRepeatOnUnknownSendCaps(filepath.Join(directory, "connector.yaml"))
			require.NoError(t, err)
			require.Equal(t, scenario.hasSendCaps, hasSendCaps)
			options, err := repeatOnUnknownGenerateOptions(filepath.Join(directory, "connector.yaml"))
			require.NoError(t, err)
			require.Equal(t, scenario.hasSendCaps, len(options) == 1)
		})
	}
	hasSendCaps, err := hasRepeatOnUnknownSendCaps(filepath.Join(t.TempDir(), "connector.yaml"))
	require.NoError(t, err)
	require.False(t, hasSendCaps, "a missing Go module keeps the older output")
}
