// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mongodb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AggregateDocumentsInput selects one collection and a read-only aggregation pipeline.
type AggregateDocumentsInput struct {
	// Collection is the collection in the configured database.
	Collection string `json:"collection"`
	// Pipeline contains read-only aggregation stages.
	Pipeline []map[string]any `json:"pipeline"`
}

// AggregateDocumentsOperation reads bounded results from a safe aggregation pipeline.
type AggregateDocumentsOperation struct{ client *Client }

// Definition returns the generated aggregation Query contract.
func (AggregateDocumentsOperation) Definition() sdkgo.QueryDefinition {
	return AggregateDocumentsDefinition
}

// Invoke validates stages and executes one read-only aggregation.
func (operation AggregateDocumentsOperation) Invoke(call sdkgo.Call, input AggregateDocumentsInput) sdkgo.QueryAttempt[DocumentSet] {
	if !collectionPattern.MatchString(input.Collection) || len(input.Pipeline) == 0 || len(input.Pipeline) > 20 {
		return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB collection or pipeline is invalid", emptyDocuments())
	}
	pipeline := make(mongo.Pipeline, 0, len(input.Pipeline)+1)
	for _, stage := range input.Pipeline {
		if len(stage) != 1 || hasJavaScriptOperator(stage) || hasWriteStage(stage) {
			return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB aggregation includes an invalid or write stage", emptyDocuments())
		}
		for name := range stage {
			if !allowedReadStage(name) {
				return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB aggregation stage is unsupported", emptyDocuments())
			}
		}
		encoded, err := json.Marshal(stage)
		if err != nil || len(encoded) > 64<<10 {
			return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB aggregation stage is invalid or too large", emptyDocuments())
		}
		var decoded bson.D
		if bson.UnmarshalExtJSON(encoded, false, &decoded) != nil {
			return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB aggregation Extended JSON is invalid", emptyDocuments())
		}
		pipeline = append(pipeline, decoded)
	}
	pipeline = append(pipeline, bson.D{{Key: "$limit", Value: operation.client.maxDocuments + 1}})
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil {
		return aggregationRetry(sdkgo.FailureTransport, "MongoDB credentials are unavailable")
	}
	connectionURI := credentials.ConnectionURI.Reveal()
	if !validConnectionURI(connectionURI) {
		return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureAuthentication, "MongoDB connection URI is invalid", emptyDocuments())
	}
	ctx, cancel := context.WithTimeout(call.Context, operation.client.timeout)
	defer cancel()
	providerClient := operation.client.mongoClient
	if providerClient == nil {
		providerClient, err = mongo.Connect(ctx, options.Client().ApplyURI(connectionURI).
			SetServerSelectionTimeout(operation.client.timeout).SetConnectTimeout(operation.client.timeout))
		if err != nil {
			return aggregationFailure(err, true)
		}
		defer func() {
			closeContext, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			_ = providerClient.Disconnect(closeContext) // Best effort after the read is complete.
		}()
	}
	cursor, err := providerClient.Database(operation.client.database).Collection(input.Collection).Aggregate(ctx, pipeline)
	if err != nil {
		return aggregationFailure(err, false)
	}
	defer func() { _ = cursor.Close(ctx) }() // Best effort after a read; no write is pending.
	result := emptyDocuments()
	var encodedBytes int64
	for cursor.Next(ctx) {
		if int64(len(result.Documents)) >= operation.client.maxDocuments {
			return aggregationBranch(AggregateDocumentsBranchTruncated, sdkgo.FailureResponseTooLarge, "MongoDB document count limit was reached", result)
		}
		encoded, err := bson.MarshalExtJSON(cursor.Current, true, false)
		if err != nil {
			return aggregationBranch(AggregateDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "MongoDB document encoding failed", emptyDocuments())
		}
		if bytes.Contains(encoded, []byte(connectionURI)) {
			return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureAuthentication, "MongoDB response contained a credential", emptyDocuments())
		}
		if encodedBytes+int64(len(encoded)) > operation.client.maxResponseBytes {
			return aggregationBranch(AggregateDocumentsBranchTruncated, sdkgo.FailureResponseTooLarge, "MongoDB response byte limit was reached", result)
		}
		var document map[string]any
		if json.Unmarshal(encoded, &document) != nil || document == nil {
			return aggregationBranch(AggregateDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "MongoDB document JSON was invalid", emptyDocuments())
		}
		result.Documents = append(result.Documents, document)
		encodedBytes += int64(len(encoded))
	}
	if err := cursor.Err(); err != nil {
		return aggregationFailure(err, false)
	}
	return sdkgo.NewQueryBranch(AggregateDocumentsBranchAggregated, result, nil, sdkgo.Receipt{})
}

func allowedReadStage(name string) bool {
	switch name {
	case "$match", "$project", "$group", "$sort", "$limit", "$skip", "$unwind", "$addFields", "$count", "$lookup", "$facet", "$replaceRoot", "$replaceWith":
		return true
	default:
		return false
	}
}

func hasWriteStage(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$out" || key == "$merge" || hasWriteStage(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasWriteStage(child) {
				return true
			}
		}
	}
	return false
}

func aggregationFailure(err error, duringConnect bool) sdkgo.QueryAttempt[DocumentSet] {
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) || mongo.IsNetworkError(err) || mongo.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
		return aggregationRetry(sdkgo.FailureTransport, "MongoDB aggregation was interrupted")
	}
	var commandError mongo.CommandError
	if errors.As(err, &commandError) {
		if commandError.HasErrorLabel("RetryableReadError") || commandError.IsMaxTimeMSExpiredError() {
			return aggregationRetry(sdkgo.FailureAvailability, "MongoDB aggregation is temporarily unavailable")
		}
		return aggregationBranch(AggregateDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "MongoDB rejected the aggregate command", emptyDocuments())
	}
	if duringConnect {
		return aggregationBranch(AggregateDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB connection URI was rejected", emptyDocuments())
	}
	return aggregationRetry(sdkgo.FailureTransport, "MongoDB aggregation failed")
}

func aggregationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, result DocumentSet) sdkgo.QueryAttempt[DocumentSet] {
	failure := sdkgo.Failure{Kind: kind, Provider: "mongodb", Operation: "aggregateDocuments", Message: message}
	return sdkgo.NewQueryBranch(branch, result, &failure, sdkgo.Receipt{})
}

func aggregationRetry(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentSet] {
	return sdkgo.NewQueryRetry[DocumentSet](sdkgo.Failure{Kind: kind, Provider: "mongodb", Operation: "aggregateDocuments", Message: message}, 0)
}
