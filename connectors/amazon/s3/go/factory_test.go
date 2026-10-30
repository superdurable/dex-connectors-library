// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type storedObjectTarget struct {
	dex.StepDefaultsNoWaitFor[s3.PutObjectResult]
}

func (storedObjectTarget) Execute(dex.Context, s3.PutObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type objectMetadataTarget struct {
	dex.StepDefaultsNoWaitFor[s3.HeadObjectResult]
}

func (objectMetadataTarget) Execute(dex.Context, s3.HeadObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathAndTheirOwnConnection(t *testing.T) {
	client, err := s3.New(s3.Config{}, staticCredentials(""))
	require.NoError(t, err)
	connection, err := s3.NewConnection(client, testConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "s3", GroupLabel: "Amazon S3", Explanation: "Store an object."}
	mapToPut := func(string) s3.PutObjectInput { return s3.PutObjectInput{} }
	require.NotPanics(t, func() {
		s3.NewPutObjectStep(s3.PutObjectStepConfig[string]{
			StepType: "Store", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: mapToPut, Stored: sdkgo.GoTo(storedObjectTarget{}),
		})
	})
	require.Panics(t, func() {
		s3.NewPutObjectStep(s3.PutObjectStepConfig[string]{
			StepType: "Store", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: mapToPut, AlreadyExists: sdkgo.GoTo(storedObjectTarget{}),
		})
	}, "the stored branch is required")
	require.Panics(t, func() {
		s3.NewPutObjectStep(s3.PutObjectStepConfig[string]{
			StepType: "Store", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToPut, Stored: sdkgo.GoTo(storedObjectTarget{}),
		})
	}, "the static connection name must match the runtime connection")
	require.NotPanics(t, func() {
		s3.NewHeadObjectStep(s3.HeadObjectStepConfig[string]{
			StepType: "Inspect", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(string) s3.HeadObjectInput { return s3.HeadObjectInput{} }, Found: sdkgo.GoTo(objectMetadataTarget{}),
		})
	})
}

// TestEveryOperationIsAsyncAndRetrySafeWithoutAnUncertainBranch: a repeated PUT stores the same bytes.
func TestEveryOperationIsAsyncAndRetrySafeWithoutAnUncertainBranch(t *testing.T) {
	for _, test := range []struct {
		operation string
		branches  []sdkgo.BranchDefinition
		defaults  sdkgo.StepDefaults
	}{
		{"listObjects", s3.ListObjectsDefinition.Branches, s3.ListObjectsDefinition.StepDefaults},
		{"headObject", s3.HeadObjectDefinition.Branches, s3.HeadObjectDefinition.StepDefaults},
		{"getObjectText", s3.GetObjectTextDefinition.Branches, s3.GetObjectTextDefinition.StepDefaults},
		{"putObject", s3.PutObjectDefinition.Branches, s3.PutObjectDefinition.StepDefaults},
	} {
		required := 0
		for _, branch := range test.branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, test.operation)
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, dex.StepDurabilityAsync, test.defaults.ExecuteDurability, test.operation)
		require.Greater(t, test.defaults.ExecuteMethodTimeout.Seconds(), 25.0, "%s: the 25-second operation deadline fits the Execute timeout", test.operation)
	}
}
