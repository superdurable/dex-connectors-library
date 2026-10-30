// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package photosearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/unsplash/go"
	"testing"
)

func TestMapToSearchPhotosInput(t *testing.T) {
	require.Equal(t, unsplash.SearchPhotosInput{Query: "office", PerPage: 5}, MapToSearchPhotosInput(Request{Query: "office", PerPage: 5}))
}
