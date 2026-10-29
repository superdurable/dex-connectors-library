// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package indexsearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/algolia"
	"testing"
)

func TestMapToSearchInput(t *testing.T) {
	require.Equal(t, algolia.SearchInput{IndexName: "products", Query: "notebook", HitsPerPage: 10}, MapToSearchInput(Request{IndexName: "products", Query: "notebook"}))
}
