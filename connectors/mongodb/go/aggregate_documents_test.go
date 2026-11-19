// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mongodb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestAggregateDocumentsReadsBoundedCanonicalJSON(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	provider.Run("aggregated", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "example.orders", mtest.FirstBatch,
			bson.D{{Key: "status", Value: "active"}, {Key: "count", Value: int32(3)}}))
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 2).AggregateDocuments(), testConnection,
			AggregateDocumentsInput{Collection: "orders", Pipeline: []map[string]any{{"$match": map[string]any{"status": "active"}}}})
		require.NoError(mt, err)
		require.Equal(mt, AggregateDocumentsBranchAggregated, result.Branch)
		require.Equal(mt, map[string]any{"$numberInt": "3"}, result.Value.Documents[0]["count"])
		started := mt.GetStartedEvent()
		require.Equal(mt, "aggregate", started.CommandName)
		require.Equal(mt, "orders", started.Command.Lookup("aggregate").StringValue())
		stages, err := started.Command.Lookup("pipeline").Array().Values()
		require.NoError(mt, err)
		require.Len(mt, stages, 2)
	})
}

func TestAggregateDocumentsTruncatesAndRejectsWriteStages(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	provider.Run("truncated", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "example.orders", mtest.FirstBatch,
			bson.D{{Key: "status", Value: "first"}}, bson.D{{Key: "status", Value: "second"}}))
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).AggregateDocuments(), testConnection,
			AggregateDocumentsInput{Collection: "orders", Pipeline: []map[string]any{{"$match": map[string]any{}}}})
		require.NoError(mt, err)
		require.Equal(mt, AggregateDocumentsBranchTruncated, result.Branch)
		require.Len(mt, result.Value.Documents, 1)
	})
	client := testClient(t, nil, 1)
	for _, pipeline := range [][]map[string]any{
		{{"$out": "other"}},
		{{"$lookup": map[string]any{"pipeline": []any{map[string]any{"$merge": "other"}}}}},
		{{"$match": map[string]any{"$where": "return true"}}},
	} {
		result, err := sdkgo.RunQuery(queryContext{}, client.AggregateDocuments(), testConnection,
			AggregateDocumentsInput{Collection: "orders", Pipeline: pipeline})
		require.NoError(t, err)
		require.Equal(t, AggregateDocumentsBranchDefect, result.Branch)
	}
}

func TestAggregateDocumentsClassifiesProviderFailures(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	input := AggregateDocumentsInput{Collection: "orders", Pipeline: []map[string]any{{"$match": map[string]any{}}}}
	provider.Run("rejected", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 13, Message: "unauthorized"}))
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).AggregateDocuments(), testConnection, input)
		require.NoError(mt, err)
		require.Equal(mt, AggregateDocumentsBranchProviderRejected, result.Branch)
	})
	provider.Run("retryable", func(mt *mtest.T) {
		failure := mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 91, Labels: []string{"RetryableReadError"}})
		mt.AddMockResponses(failure, failure)
		_, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).AggregateDocuments(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(mt, err, &retry)
	})
}
