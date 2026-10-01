// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createTarget struct {
	dex.StepDefaultsNoWaitFor[docs.CreateDocumentResult]
}

func (createTarget) Execute(dex.Context, docs.CreateDocumentResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type readTarget struct {
	dex.StepDefaultsNoWaitFor[docs.GetDocumentTextResult]
}

func (readTarget) Execute(dex.Context, docs.GetDocumentTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

var testAnnotations = sdkgo.StepAnnotations{GroupID: "google", GroupLabel: "Google", Explanation: "Use Google Docs."}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := docs.NewConnection(newDocsClient(t, "http://127.0.0.1:1"), docsConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		docs.NewCreateDocumentStep(docs.CreateDocumentStepConfig[string]{
			StepType: "Create", Annotations: testAnnotations, Connection: connection, ConnectionName: "policy-docs",
			MapToOperationInput: func(string) docs.CreateDocumentInput { return docs.CreateDocumentInput{} },
			Created:             sdkgo.GoTo(createTarget{}),
		})
		docs.NewGetDocumentTextStep(docs.GetDocumentTextStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) docs.GetDocumentTextInput { return docs.GetDocumentTextInput{} },
			Read:                sdkgo.GoTo(readTarget{}), TooLarge: sdkgo.GoTo(readTarget{}),
		})
	})
	require.Panics(t, func() {
		docs.NewCreateDocumentStep(docs.CreateDocumentStepConfig[string]{
			StepType: "Create", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) docs.CreateDocumentInput { return docs.CreateDocumentInput{} },
			Uncertain:           sdkgo.GoTo(createTarget{}),
		})
	})
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection, err := docs.NewConnection(newDocsClient(t, "http://127.0.0.1:1"), docsConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		docs.NewGetDocumentTextStep(docs.GetDocumentTextStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) docs.GetDocumentTextInput { return docs.GetDocumentTextInput{} },
			Read:                sdkgo.GoTo(readTarget{}),
		})
	})
}

func TestDocsConnectionCannotBeSerialized(t *testing.T) {
	connection, err := docs.NewConnection(newDocsClient(t, "http://127.0.0.1:1"), docsConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), docsTestToken)
	require.Equal(t, "docs.Connection{[REDACTED]}", connection.String())
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[docs.Credentials]{}
	for name, config := range map[string]docs.Config{
		"plain HTTP Docs endpoint":  {DocsEndpoint: "http://docs.example.com"},
		"plain HTTP Drive endpoint": {DriveEndpoint: "http://drive.example.com"},
		"text limit above 4 MiB":    {MaxTextBytes: 4<<20 + 1},
		"negative response limit":   {MaxResponseBytes: -1},
	} {
		_, err := docs.New(config, credentials)
		require.Error(t, err, name)
	}
	_, err := docs.New(docs.Config{}, nil)
	require.Error(t, err)
	client, err := docs.New(docs.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}
