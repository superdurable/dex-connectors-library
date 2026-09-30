// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type uploadTarget struct {
	dex.StepDefaultsNoWaitFor[drive.UploadFileResult]
}

func (uploadTarget) Execute(dex.Context, drive.UploadFileResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type searchTarget struct {
	dex.StepDefaultsNoWaitFor[drive.SearchFilesResult]
}

func (searchTarget) Execute(dex.Context, drive.SearchFilesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

var testAnnotations = sdkgo.StepAnnotations{GroupID: "google", GroupLabel: "Google", Explanation: "Use Google Drive."}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := drive.NewConnection(newDriveClient(t, "http://127.0.0.1:1"), driveConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		drive.NewUploadFileStep(drive.UploadFileStepConfig[string]{
			StepType: "Upload", Annotations: testAnnotations, Connection: connection, ConnectionName: "drive-files",
			MapToOperationInput: func(string) drive.UploadFileInput { return drive.UploadFileInput{} },
			Uploaded:            sdkgo.GoTo(uploadTarget{}),
		})
		drive.NewSearchFilesStep(drive.SearchFilesStepConfig[string]{
			StepType: "Search", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) drive.SearchFilesInput { return drive.SearchFilesInput{} },
			Found:               sdkgo.GoTo(searchTarget{}), NotFound: sdkgo.GoTo(searchTarget{}),
		})
	})
	require.Panics(t, func() {
		drive.NewUploadFileStep(drive.UploadFileStepConfig[string]{
			StepType: "Upload", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) drive.UploadFileInput { return drive.UploadFileInput{} },
			Uncertain:           sdkgo.GoTo(uploadTarget{}),
		})
	})
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection, err := drive.NewConnection(newDriveClient(t, "http://127.0.0.1:1"), driveConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		drive.NewSearchFilesStep(drive.SearchFilesStepConfig[string]{
			StepType: "Search", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) drive.SearchFilesInput { return drive.SearchFilesInput{} },
			Found:               sdkgo.GoTo(searchTarget{}),
		})
	})
}

func TestDriveConnectionCannotBeSerialized(t *testing.T) {
	connection, err := drive.NewConnection(newDriveClient(t, "http://127.0.0.1:1"), driveConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), driveTestToken)
	require.Equal(t, "drive.Connection{[REDACTED]}", connection.String())
}
