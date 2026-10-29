// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package coheresearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/cohere"
	"github.com/superdurable/dex-connectors-library/connectors/cohere/coheremock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestOperationMapping(t *testing.T) {
	request := Request{Query: "release", Documents: []string{"release note", "weather"}}
	require.Equal(t, cohere.EmbedInput{Model: "embed-v4.0", Texts: []string{"release"}, InputType: "search_query"}, MapToEmbedInput(request))
	require.Equal(t, cohere.RerankInput{Model: "rerank-v5.0-pro", Query: "release", Documents: request.Documents}, MapToRerankInput(request))
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := coheremock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
