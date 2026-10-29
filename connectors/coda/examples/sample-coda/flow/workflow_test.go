// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/coda"
	"testing"
)

func TestMapToOperationInputPreservesRequest(t *testing.T) {
	request := coda.ListRowsInput{DocID: "AbCDeFGH", TableID: "grid-pqRst-U", Limit: 10}
	require.Equal(t, request, MapToOperationInput(request))
}
