// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postcreation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go"
	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go/wordpressmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPostCreationIdentityAndMapping(t *testing.T) {
	input := wordpress.CreatePostInput{Title: "Meridian release", Content: "<p>Draft.</p>"}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wordpress.Connection{})))
	require.Equal(t, "StartCreatePost", dex.GetFinalStepType[wordpress.CreatePostInput](startRequest{}))
	mock := wordpressmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
