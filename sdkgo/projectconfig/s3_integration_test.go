//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

type configurationWriteResult struct{ err error }
type exchangeAdmissionResult struct {
	admission projectconfig.ExchangeAdmission
	err       error
}

func TestAWSProjectConfigurationSnapshotsAndConcurrentAdmission(t *testing.T) {
	bucket, kmsKey := os.Getenv("PROJECTCONFIG_TEST_BUCKET"), os.Getenv("PROJECTCONFIG_TEST_KMS_KEY_ARN")
	require.NotEmpty(t, bucket, "real AWS integration requires an explicit existing versioned bucket")
	require.NotEmpty(t, kmsKey, "real AWS integration requires the exact existing KMS key ARN")
	require.NotEmpty(t, os.Getenv("AWS_REGION"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	awsConfig, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err)
	client := s3.NewFromConfig(awsConfig)
	operationID := uuid.NewString()
	prefix := "dex-projectconfig-integration/" + operationID
	t.Logf("Owned AWS storage test prefix: %s", prefix)
	t.Cleanup(func() { deleteOwnedVersions(t, client, bucket, prefix+"/") })
	scope := projectconfig.Scope{ProjectID: operationID, Kind: "live"}
	stores := make([]*projectconfig.ConfigurationStore, 8)
	connectionStores := make([]*projectconfig.ConnectionStore, 8)
	for index := range stores {
		independentClient := s3.NewFromConfig(awsConfig)
		objects, err := projectconfig.NewS3ObjectStore(ctx, &projectconfig.S3StoreConfig{Client: independentClient, Bucket: bucket, Prefix: prefix, KMSKeyID: kmsKey})
		require.NoError(t, err)
		stores[index], err = projectconfig.NewConfigurationStore(&projectconfig.ConfigurationStoreConfig{Objects: objects, Scope: scope})
		require.NoError(t, err)
		connectionStores[index], err = projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: objects, Scope: scope})
		require.NoError(t, err)
	}
	initialValue := "initial"
	initial, _, err := stores[0].WriteConfiguration(ctx, 0, projectconfig.Configuration{Environment: map[string]projectconfig.EnvironmentValue{"SDK_PROBE_SETTING": {Value: &initialValue}}})
	require.NoError(t, err)
	require.EqualValues(t, 1, initial.Revision)
	snapshot, err := stores[0].FreezeConfiguration(ctx, 1)
	require.NoError(t, err)
	results := make(chan configurationWriteResult, len(stores))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index, store := range stores {
		workers.Add(1)
		go writeConfigurationCandidate(ctx, store, index, start, results, &workers)
	}
	close(start)
	workers.Wait()
	close(results)
	acceptedWrites := 0
	for result := range results {
		if result.err == nil {
			acceptedWrites++
		} else {
			require.ErrorIs(t, result.err, projectconfig.ErrConflict)
		}
	}
	require.Equal(t, 1, acceptedWrites)
	current, _, err := stores[7].ReadConfiguration(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, current.Revision)
	historical, err := stores[3].ReadSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.EqualValues(t, 1, historical.Revision)
	require.Equal(t, initialValue, *historical.Environment["SDK_PROBE_SETTING"].Value)
	objects, err := projectconfig.NewS3ObjectStore(ctx, &projectconfig.S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, KMSKeyID: kmsKey})
	require.NoError(t, err)
	foreign, err := projectconfig.NewConfigurationStore(&projectconfig.ConfigurationStoreConfig{Objects: objects, Scope: projectconfig.Scope{ProjectID: uuid.NewString(), Kind: "live"}})
	require.NoError(t, err)
	_, err = foreign.ReadSnapshot(ctx, snapshot)
	require.Error(t, err)
	key := projectconfig.ConnectionKey{ConnectorID: "storage-probe", ConnectionName: "primary"}
	attemptID := uuid.NewString()
	deadline := time.Now().Add(time.Minute)
	admissions := make(chan exchangeAdmissionResult, len(connectionStores))
	start = make(chan struct{})
	for _, store := range connectionStores {
		workers.Add(1)
		go admitCredentialExchange(ctx, store, key, attemptID, deadline, start, admissions, &workers)
	}
	close(start)
	workers.Wait()
	close(admissions)
	dispatchPermissions := 0
	var winner projectconfig.ExchangeAdmission
	for result := range admissions {
		require.NoError(t, result.err)
		if result.admission.ProviderDispatchAllowed {
			dispatchPermissions++
			winner = result.admission
		}
	}
	require.Equal(t, 1, dispatchPermissions)
	_, err = connectionStores[7].RecoverCredentialExchange(ctx, winner)
	require.ErrorIs(t, err, projectconfig.ErrExchangePending)
	material := projectconfig.CredentialMaterial{Credentials: json.RawMessage(`{"storageProbe":"non-provider-test-data"}`), AuthMethod: "api-key"}
	committed, err := connectionStores[0].CommitCredentialExchange(ctx, winner, material)
	require.NoError(t, err)
	require.Equal(t, projectconfig.CredentialReady, committed.Status)
	recovered, err := connectionStores[6].RecoverCredentialExchange(ctx, winner)
	require.NoError(t, err)
	require.Equal(t, committed.Revision, recovered.Revision)
	private, _, err := connectionStores[4].ReadCredentialMaterial(ctx, key)
	require.NoError(t, err)
	require.Equal(t, string(material.Credentials), string(private.Credentials))
	_, err = json.Marshal(private)
	require.Error(t, err)
	_, err = connectionStores[1].ReplaceCredential(ctx, key, 0, material)
	require.True(t, errors.Is(err, projectconfig.ErrConflict))
	for name, value := range map[string]string{"DEX_PROJECT_ID": scope.ProjectID, "DEX_PROJECT_SCOPE": scope.Kind, "DEX_PROJECT_SESSION_ID": "", "DEX_PROJECT_CONFIG_KEY": snapshot.Key, "DEX_PROJECT_CONFIG_VERSION": snapshot.Version, "DEX_PROJECT_CONFIG_DIGEST": snapshot.Digest, "DEX_PROJECT_STORAGE_BUCKET": bucket, "DEX_PROJECT_STORAGE_PREFIX": prefix, "DEX_PROJECT_STORAGE_KMS_KEY_ARN": kmsKey, "DEX_PROJECT_ALLOW_LOCAL_STORAGE": "", "DEX_PROJECT_STORAGE_ENDPOINT": ""} {
		t.Setenv(name, value)
	}
	loaded, err := projectconfig.LoadFromEnvironment(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, loaded.Configuration.Revision)
	t.Logf("Real AWS storage verified: operation=%s concurrent_writes=%d dispatch_permissions=%d exact_snapshot=%s; provider calls were not performed", operationID, acceptedWrites, dispatchPermissions, snapshot.Version)
}

func writeConfigurationCandidate(ctx context.Context, store *projectconfig.ConfigurationStore, index int, start <-chan struct{}, results chan<- configurationWriteResult, workers *sync.WaitGroup) {
	defer workers.Done()
	<-start
	value := fmt.Sprintf("candidate-%d", index)
	_, _, err := store.WriteConfiguration(ctx, 1, projectconfig.Configuration{Environment: map[string]projectconfig.EnvironmentValue{"SDK_PROBE_SETTING": {Value: &value}}})
	results <- configurationWriteResult{err: err}
}

func admitCredentialExchange(ctx context.Context, store *projectconfig.ConnectionStore, key projectconfig.ConnectionKey, attemptID string, deadline time.Time, start <-chan struct{}, results chan<- exchangeAdmissionResult, workers *sync.WaitGroup) {
	defer workers.Done()
	<-start
	admission, err := store.BeginCredentialExchange(ctx, key, 0, attemptID, deadline)
	results <- exchangeAdmissionResult{admission: admission, err: err}
}

func deleteOwnedVersions(t *testing.T, client *s3.Client, bucket, prefix string) {
	t.Helper()
	require.True(t, strings.HasPrefix(prefix, "dex-projectconfig-integration/") && strings.Count(prefix, "/") == 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	paginator := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	deletedCount := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		require.NoError(t, err)
		var objects []types.ObjectIdentifier
		for _, version := range page.Versions {
			require.True(t, strings.HasPrefix(aws.ToString(version.Key), prefix))
			require.NotEmpty(t, aws.ToString(version.VersionId))
			objects = append(objects, types.ObjectIdentifier{Key: version.Key, VersionId: version.VersionId})
		}
		for _, marker := range page.DeleteMarkers {
			require.True(t, strings.HasPrefix(aws.ToString(marker.Key), prefix))
			require.NotEmpty(t, aws.ToString(marker.VersionId))
			objects = append(objects, types.ObjectIdentifier{Key: marker.Key, VersionId: marker.VersionId})
		}
		if len(objects) > 0 {
			result, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket), Delete: &types.Delete{Objects: objects}})
			require.NoError(t, err)
			require.Empty(t, result.Errors)
			deletedCount += len(objects)
		}
	}
	remaining, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	require.NoError(t, err)
	require.False(t, aws.ToBool(remaining.IsTruncated))
	require.Empty(t, remaining.Versions)
	require.Empty(t, remaining.DeleteMarkers)
	t.Logf("Deleted and confirmed absence of %d exact versions under owned prefix %s", deletedCount, prefix)
}
