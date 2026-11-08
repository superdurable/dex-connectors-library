// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go"
	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go/opensearchmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentSearchIdentityAndMapping(t *testing.T) {
	input := opensearch.SearchDocumentsInput{Index: "products", Query: "title:Meridian", Size: 10}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(opensearch.Connection{})))
	require.Equal(t, "StartSearchDocuments", dex.GetFinalStepType[opensearch.SearchDocumentsInput](startRequest{}))
	mock := opensearchmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
