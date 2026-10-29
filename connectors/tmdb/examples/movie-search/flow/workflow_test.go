// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moviesearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tmdb"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	require.Equal(t, "TMDBMovieSearch", dex.GetFinalFlowType(NewFlow(tmdb.Connection{})))
	require.Equal(t, "PrepareRequest", dex.GetFinalStepType[Request](prepareRequest{}))
	require.Equal(t, "CompleteRequest", dex.GetFinalStepType[tmdb.SearchMoviesResult](completeRequest{}))
	request := Request(tmdb.SearchMoviesInput{Query: "Jack Reacher", Page: 1})
	mapped := tmdb.SearchMoviesInput(request)
	require.Equal(t, request, Request(mapped))
	wait, err := prepareRequest{}.WaitFor(nil, request)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	reference := sdkgo.ConnectionRef{Provider: "tmdb", Name: ConnectionName}
	mockConnection, err := tmdb.NewConnectionWithOperations(nil, reference)
	require.Error(t, err)
	require.Equal(t, tmdb.Connection{}, mockConnection)
}
