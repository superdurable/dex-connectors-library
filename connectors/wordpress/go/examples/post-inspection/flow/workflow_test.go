// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go"
	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go/wordpressmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPostInspectionIdentityAndMapping(t *testing.T) {
	input := wordpress.GetPostInput{PostID: 42}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wordpress.Connection{})))
	require.Equal(t, "StartGetPost", dex.GetFinalStepType[wordpress.GetPostInput](startRequest{}))
	mock := wordpressmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
