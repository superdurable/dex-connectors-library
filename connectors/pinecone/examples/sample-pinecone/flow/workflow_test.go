// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pinecone"
	"testing"
)

func TestMapToOperationInputPreservesRequest(t *testing.T) {
	request := pinecone.UpsertVectorsInput{Namespace: "example", Vectors: []pinecone.Vector{{ID: "vec-meridian", Values: []float32{0.1, 0.2}}}}
	require.Equal(t, request, MapToOperationInput(request))
}
