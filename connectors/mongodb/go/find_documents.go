// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mongodb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FindDocumentsInput names one collection and supplies a JSON query filter.
type FindDocumentsInput struct {
	// Collection is a non-secret name in the configured database.
	Collection string `json:"collection"`
	// Filter is a JSON object; MongoDB Extended JSON values preserve BSON types.
	Filter map[string]any `json:"filter"`
}

// DocumentSet contains bounded canonical Extended JSON documents.
type DocumentSet struct {
	// Documents contains JSON objects with BSON types represented by Extended JSON wrappers.
	Documents []map[string]any `json:"documents"`
}

// FindDocumentsOperation reads documents without modifying MongoDB.
type FindDocumentsOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (FindDocumentsOperation) Definition() sdkgo.QueryDefinition { return FindDocumentsDefinition }

// Invoke resolves credentials and executes one bounded find.
func (operation FindDocumentsOperation) Invoke(call sdkgo.Call, input FindDocumentsInput) sdkgo.QueryAttempt[DocumentSet] {
	if !collectionPattern.MatchString(input.Collection) || hasJavaScriptOperator(input.Filter) {
		return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB collection or filter is invalid", emptyDocuments())
	}
	filterJSON, err := json.Marshal(input.Filter)
	if err != nil || len(filterJSON) > 64<<10 {
		return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB filter is invalid or too large", emptyDocuments())
	}
	var filter bson.M
	if err := bson.UnmarshalExtJSON(filterJSON, false, &filter); err != nil {
		return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB filter Extended JSON is invalid", emptyDocuments())
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil {
		return documentRetry(sdkgo.FailureTransport, "MongoDB credentials are unavailable")
	}
	connectionURI := credentials.ConnectionURI.Reveal()
	if !validConnectionURI(connectionURI) {
		return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureAuthentication, "MongoDB connection URI is invalid", emptyDocuments())
	}
	ctx, cancel := context.WithTimeout(call.Context, operation.client.timeout)
	defer cancel()
	providerClient := operation.client.mongoClient
	if providerClient == nil {
		providerClient, err = mongo.Connect(ctx, options.Client().ApplyURI(connectionURI).
			SetServerSelectionTimeout(operation.client.timeout).SetConnectTimeout(operation.client.timeout))
		if err != nil {
			return connectFailure(err)
		}
		defer func() {
			closeContext, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			_ = providerClient.Disconnect(closeContext) // Best effort after a read; the Flow result already reflects the query.
		}()
	}
	cursor, err := providerClient.Database(operation.client.database).Collection(input.Collection).Find(
		ctx, filter, options.Find().SetLimit(operation.client.maxDocuments+1),
	)
	if err != nil {
		return documentFailure(err)
	}
	defer func() { _ = cursor.Close(ctx) }() // Best effort after a read; no write can be left pending.
	result := emptyDocuments()
	var encodedBytes int64
	for cursor.Next(ctx) {
		if int64(len(result.Documents)) >= operation.client.maxDocuments {
			return documentBranch(FindDocumentsBranchTruncated, sdkgo.FailureResponseTooLarge, "MongoDB document count limit was reached", result)
		}
		encoded, err := bson.MarshalExtJSON(cursor.Current, true, false)
		if err != nil {
			return documentBranch(FindDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "MongoDB document encoding failed", emptyDocuments())
		}
		if bytes.Contains(encoded, []byte(connectionURI)) {
			return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureAuthentication, "MongoDB response contained a credential", emptyDocuments())
		}
		if encodedBytes+int64(len(encoded)) > operation.client.maxResponseBytes {
			return documentBranch(FindDocumentsBranchTruncated, sdkgo.FailureResponseTooLarge, "MongoDB response byte limit was reached", result)
		}
		var document map[string]any
		if err := json.Unmarshal(encoded, &document); err != nil || document == nil {
			return documentBranch(FindDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "MongoDB document JSON was invalid", emptyDocuments())
		}
		result.Documents = append(result.Documents, document)
		encodedBytes += int64(len(encoded))
	}
	if err := cursor.Err(); err != nil {
		return documentFailure(err)
	}
	return sdkgo.NewQueryBranch(FindDocumentsBranchFound, result, nil, sdkgo.Receipt{})
}

func validConnectionURI(connectionURI string) bool {
	return len(connectionURI) <= 4096 && !strings.ContainsAny(connectionURI, "\x00\r\n") &&
		(strings.HasPrefix(connectionURI, "mongodb://") || strings.HasPrefix(connectionURI, "mongodb+srv://"))
}

func hasJavaScriptOperator(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$where" || key == "$function" || key == "$accumulator" || hasJavaScriptOperator(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasJavaScriptOperator(child) {
				return true
			}
		}
	}
	return false
}

func documentFailure(err error) sdkgo.QueryAttempt[DocumentSet] {
	if mongo.IsNetworkError(err) || mongo.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
		return documentRetry(sdkgo.FailureTransport, "MongoDB read was interrupted")
	}
	var commandError mongo.CommandError
	if errors.As(err, &commandError) {
		if commandError.HasErrorLabel("RetryableReadError") || commandError.IsMaxTimeMSExpiredError() {
			return documentRetry(sdkgo.FailureAvailability, "MongoDB read is temporarily unavailable")
		}
		return documentBranch(FindDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "MongoDB rejected the find command", emptyDocuments())
	}
	return documentRetry(sdkgo.FailureTransport, "MongoDB read failed")
}

// connectFailure separates URI defects from DNS lookups, which ApplyURI performs for mongodb+srv:// URIs.
func connectFailure(err error) sdkgo.QueryAttempt[DocumentSet] {
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return documentRetry(sdkgo.FailureTransport, "MongoDB DNS lookup failed")
	}
	if mongo.IsNetworkError(err) || mongo.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
		return documentRetry(sdkgo.FailureTransport, "MongoDB read was interrupted")
	}
	return documentBranch(FindDocumentsBranchDefect, sdkgo.FailureValidation, "MongoDB connection URI was rejected", emptyDocuments())
}

func emptyDocuments() DocumentSet { return DocumentSet{Documents: []map[string]any{}} }

func documentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, result DocumentSet) sdkgo.QueryAttempt[DocumentSet] {
	failure := sdkgo.Failure{Kind: kind, Provider: "mongodb", Operation: "findDocuments", Message: message}
	return sdkgo.NewQueryBranch(branch, result, &failure, sdkgo.Receipt{})
}

func documentRetry(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentSet] {
	return sdkgo.NewQueryRetry[DocumentSet](sdkgo.Failure{
		Kind: kind, Provider: "mongodb", Operation: "findDocuments", Message: message,
	}, 0)
}
