// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go"
	"github.com/superdurable/dex-connectors-library/connectors/elastic/elasticsearch/go/elasticsearchmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentSearchIdentityAndMapping(t *testing.T) {
	input := elasticsearch.SearchDocumentsInput{Index: "products", Query: "title:Meridian", Size: 10}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(elasticsearch.Connection{})))
	require.Equal(t, "StartSearchDocuments", dex.GetFinalStepType[elasticsearch.SearchDocumentsInput](startRequest{}))
	mock := elasticsearchmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
