// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/contentful"
	"testing"
)

func TestMapToOperationInputPreservesRequest(t *testing.T) {
	request := contentful.GetEntryInput{SpaceID: "space123", EnvironmentID: "master", EntryID: "entry-meridian"}
	require.Equal(t, request, MapToOperationInput(request))
}
