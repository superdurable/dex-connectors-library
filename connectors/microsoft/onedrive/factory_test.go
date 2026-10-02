// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testAnnotations = sdkgo.StepAnnotations{GroupID: "microsoft", GroupLabel: "Microsoft", Explanation: "Use OneDrive."}

type getTarget struct {
	dex.StepDefaultsNoWaitFor[onedrive.GetFileResult]
}

func (getTarget) Execute(dex.Context, onedrive.GetFileResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type uploadTarget struct {
	dex.StepDefaultsNoWaitFor[onedrive.UploadFileResult]
}

func (uploadTarget) Execute(dex.Context, onedrive.UploadFileResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type folderTarget struct {
	dex.StepDefaultsNoWaitFor[onedrive.CreateFolderResult]
}

func (folderTarget) Execute(dex.Context, onedrive.CreateFolderResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func newTestConnection(t *testing.T) onedrive.Connection {
	t.Helper()
	connection, err := onedrive.NewConnection(newGraphClient(t, "http://127.0.0.1:1"), graphConnection)
	require.NoError(t, err)
	return connection
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	require.NotPanics(t, func() {
		onedrive.NewUploadFileStep(onedrive.UploadFileStepConfig[string]{
			StepType: "Upload", Annotations: testAnnotations, Connection: connection, ConnectionName: graphConnection.Name,
			MapToOperationInput: func(string) onedrive.UploadFileInput { return onedrive.UploadFileInput{} },
			Uploaded:            sdkgo.GoTo(uploadTarget{}),
		})
		onedrive.NewCreateFolderStep(onedrive.CreateFolderStepConfig[string]{
			StepType: "Folder", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) onedrive.CreateFolderInput { return onedrive.CreateFolderInput{} },
			Created:             sdkgo.GoTo(folderTarget{}),
		})
	})
	require.Panics(t, func() {
		onedrive.NewUploadFileStep(onedrive.UploadFileStepConfig[string]{
			StepType: "Upload", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) onedrive.UploadFileInput { return onedrive.UploadFileInput{} },
			AlreadyExists:       sdkgo.GoTo(uploadTarget{}),
		})
	})
}

func TestMutationsDeclareNoUncertainBranch(t *testing.T) {
	for _, definition := range []sdkgo.MutationDefinition{onedrive.UploadFileDefinition, onedrive.CreateFolderDefinition} {
		for _, branch := range definition.Branches {
			require.NotEqual(t, sdkgo.BranchID("uncertain"), branch.ID, "a PUT or POST by name never duplicates, so every unknown outcome retries")
		}
	}
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection := newTestConnection(t)
	require.Panics(t, func() {
		onedrive.NewGetFileStep(onedrive.GetFileStepConfig[string]{
			StepType: "Get", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) onedrive.GetFileInput { return onedrive.GetFileInput{} },
			Found:               sdkgo.GoTo(getTarget{}),
		})
	})
}

func TestConnectionCannotBeSerialized(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), graphTestToken)
	require.Equal(t, "onedrive.Connection{[REDACTED]}", connection.String())
}

func TestNewRejectsLimitsOutsideTheirRanges(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[onedrive.Credentials]{}
	for _, config := range []onedrive.Config{
		{MaxUploadBytes: 5<<20 + 1}, {SearchPageSize: 201}, {Endpoint: "http://graph.example.com"}, {Endpoint: "https://graph.example.com/?x=1"},
	} {
		_, err := onedrive.New(config, credentials)
		require.Error(t, err)
	}
	_, err := onedrive.New(onedrive.Config{}, nil)
	require.Error(t, err)
}
