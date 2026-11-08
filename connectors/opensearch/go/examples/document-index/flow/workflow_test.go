// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentindex

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go"
	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go/opensearchmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestIndexDocumentIdentityAndMapping(t *testing.T) {
	input := opensearch.IndexDocumentInput{Index: "products", DocumentID: "sku-42", Source: []byte(`{"title":"Meridian"}`)}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(opensearch.Connection{})))
	require.Equal(t, "StartIndexDocument", dex.GetFinalStepType[opensearch.IndexDocumentInput](startRequest{}))
	mock := opensearchmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
