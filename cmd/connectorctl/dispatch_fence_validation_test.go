// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestValidateDispatchFenceSource(t *testing.T) {
	directory := t.TempDir()
	goDirectory := filepath.Join(directory, "go")
	require.NoError(t, os.Mkdir(goDirectory, 0o700))
	source := `package connector
func invoke() { singledispatch.NewFence(call, CreateThingDefinition.StepDefaults.ExecuteMethodTimeout) }
`
	require.NoError(t, os.WriteFile(filepath.Join(goDirectory, "create_thing.go"), []byte(source), 0o600))
	manifest := schema.Manifest{}
	manifest.Spec.Operations = []schema.Operation{{Name: "createThing", GoName: "CreateThing"}}
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.ErrorContains(t, validateDispatchFenceSource(manifestPath, manifest), "createThing: singledispatch.NewFence requires execution.dispatchFence: true")
	manifest.Spec.Operations[0].Execution.DispatchFence = true
	require.NoError(t, validateDispatchFenceSource(manifestPath, manifest))
}
