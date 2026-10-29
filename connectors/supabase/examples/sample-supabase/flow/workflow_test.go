// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/supabase"
	"testing"
)

func TestMapToOperationInputPreservesRequest(t *testing.T) {
	request := supabase.SelectRowsInput{Table: "projects", Limit: 10}
	require.Equal(t, request, MapToOperationInput(request))
}
