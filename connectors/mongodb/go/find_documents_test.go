// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mongodb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

var testConnection = sdkgo.ConnectionRef{Provider: "mongodb", Name: "mongodb"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "mongodb-provider-test" }
func (queryContext) StepExecutionID() string     { return "find-documents" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testClient(t *testing.T, providerClient *mongo.Client, maxDocuments int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {ConnectionURI: sdkgo.NewSecretString("mongodb://reader:secret@localhost:27017")},
	}
	client, err := newClient(Config{Database: "example", MaxDocuments: maxDocuments}, credentials, providerClient)
	require.NoError(t, err)
	return client
}

func TestFindDocumentsReadsCanonicalExtendedJSON(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	provider.Run("found", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "example.orders", mtest.FirstBatch,
			bson.D{{Key: "status", Value: "active"}, {Key: "count", Value: int32(3)}}))
		input := FindDocumentsInput{Collection: "orders", Filter: map[string]any{"status": "active"}}
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 2).FindDocuments(), testConnection, input)
		require.NoError(mt, err)
		require.Equal(mt, FindDocumentsBranchFound, result.Branch)
		require.Equal(mt, "active", result.Value.Documents[0]["status"])
		require.Equal(mt, map[string]any{"$numberInt": "3"}, result.Value.Documents[0]["count"])
		started := mt.GetStartedEvent()
		require.Equal(mt, "find", started.CommandName)
		require.Equal(mt, "orders", started.Command.Lookup("find").StringValue())
		require.Equal(mt, int64(3), started.Command.Lookup("limit").Int64())
	})
}

func TestFindDocumentsTruncatesBeyondConfiguredCount(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	provider.Run("truncated", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "example.orders", mtest.FirstBatch,
			bson.D{{Key: "status", Value: "first"}}, bson.D{{Key: "status", Value: "second"}}))
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).FindDocuments(), testConnection,
			FindDocumentsInput{Collection: "orders", Filter: map[string]any{}})
		require.NoError(mt, err)
		require.Equal(mt, FindDocumentsBranchTruncated, result.Branch)
		require.Len(mt, result.Value.Documents, 1)
	})
}

func TestFindDocumentsRejectsUnsafeFilterBeforeProviderCall(t *testing.T) {
	client := testClient(t, nil, 1)
	result, err := sdkgo.RunQuery(queryContext{}, client.FindDocuments(), testConnection,
		FindDocumentsInput{Collection: "orders", Filter: map[string]any{"$where": "return true"}})
	require.NoError(t, err)
	require.Equal(t, FindDocumentsBranchDefect, result.Branch)
}

func TestFindDocumentsClassifiesMongoDBFailures(t *testing.T) {
	provider := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	provider.Run("rejected", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 13, Message: "unauthorized"}))
		result, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).FindDocuments(), testConnection,
			FindDocumentsInput{Collection: "orders"})
		require.NoError(mt, err)
		require.Equal(mt, FindDocumentsBranchProviderRejected, result.Branch)
	})
	provider.Run("retryable", func(mt *mtest.T) {
		failure := mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 91, Labels: []string{"RetryableReadError"}})
		mt.AddMockResponses(failure, failure)
		_, err := sdkgo.RunQuery(queryContext{}, testClient(mt.T, mt.Client, 1).FindDocuments(), testConnection,
			FindDocumentsInput{Collection: "orders"})
		var retry *sdkgo.RetryError
		require.ErrorAs(mt, err, &retry)
	})
}
