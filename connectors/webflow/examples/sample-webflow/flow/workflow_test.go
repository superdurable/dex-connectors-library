// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/webflow"
	"testing"
)

func TestMapToOperationInputPreservesRequest(t *testing.T) {
	request := webflow.ListCollectionItemsInput{CollectionID: "62b720ef280c7a7a3be8cabe", Limit: 10}
	require.Equal(t, request, MapToOperationInput(request))
}
