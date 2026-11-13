// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jingdigital/go"
	"github.com/superdurable/dex-connectors-library/connectors/jingdigital/go/jingdigitalmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTagCatalogFlowIdentityAndMapping(t *testing.T) {
	input := jingdigital.ListTagsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jingdigital.Connection{})))
	require.Equal(t, "StartListTags", dex.GetFinalStepType[jingdigital.ListTagsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jingdigitalmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
