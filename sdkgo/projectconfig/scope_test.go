// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestScopePrefixKeysLiveAndPreviewByProjectOnly(t *testing.T) {
	livePrefix, err := projectconfig.Scope{ProjectID: "project-1", Kind: "live"}.Prefix()
	require.NoError(t, err)
	require.Equal(t, "projects/project-1/live", livePrefix)

	previewPrefix, err := projectconfig.Scope{ProjectID: "project-1", Kind: "preview"}.Prefix()
	require.NoError(t, err)
	require.Equal(t, "projects/project-1/preview", previewPrefix)
}

func TestScopePrefixRejectsAnInvalidProjectOrKind(t *testing.T) {
	for name, scope := range map[string]projectconfig.Scope{
		"empty project":      {Kind: "live"},
		"path-like project":  {ProjectID: "../other", Kind: "live"},
		"nested project":     {ProjectID: "a/b", Kind: "preview"},
		"empty kind":         {ProjectID: "project-1"},
		"unknown kind":       {ProjectID: "project-1", Kind: "staging"},
		"capitalized kind":   {ProjectID: "project-1", Kind: "Preview"},
		"project with space": {ProjectID: "project 1", Kind: "live"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := scope.Prefix()
			require.Error(t, err)
		})
	}
}
