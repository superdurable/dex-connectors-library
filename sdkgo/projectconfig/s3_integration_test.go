//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

func TestVersionedS3ReplicaAdmissionAndRestartRecovery(t *testing.T) {
	endpoint := os.Getenv("PROJECTCONFIG_TEST_S3_ENDPOINT")
	require.NotEmpty(t, endpoint, "integration requires an explicitly configured isolated S3 fixture")
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.True(t, parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost", "this test creates and cleans a local fixture bucket only")
	accessKey, secretKey := os.Getenv("PROJECTCONFIG_TEST_S3_ACCESS_KEY"), os.Getenv("PROJECTCONFIG_TEST_S3_SECRET_KEY")
	require.NotEmpty(t, accessKey)
	require.NotEmpty(t, secretKey)
	config := aws.Config{Region: "us-east-1", Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey}, nil
	})}
	client := s3.NewFromConfig(config, func(options *s3.Options) { options.BaseEndpoint = aws.String(endpoint); options.UsePathStyle = true })
	bucket := fmt.Sprintf("sv2-projectconfig-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupFixtureBucket(t, client, bucket) })
	_, err = NewS3ObjectStore(ctx, &S3StoreConfig{Client: client, Bucket: bucket, AllowUnencrypted: true})
	require.Error(t, err, "unversioned storage must fail closed")
	_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}})
	require.NoError(t, err)
	stores := make([]*ConnectionStore, 8)
	for index := range stores {
		independentClient := s3.NewFromConfig(config, func(options *s3.Options) { options.BaseEndpoint = aws.String(endpoint); options.UsePathStyle = true })
		objects, err := NewS3ObjectStore(ctx, &S3StoreConfig{Client: independentClient, Bucket: bucket, Prefix: "replicas", AllowUnencrypted: true})
		require.NoError(t, err)
		stores[index] = newTestStore(t, objects)
	}
	expired := time.Now().Add(-time.Minute)
	_, err = stores[0].ReplaceCredential(ctx, testKey, 0, testMaterial("fixture-expired", &expired))
	require.NoError(t, err)
	driver := &testDriver{entered: make(chan struct{}, 8), proceed: make(chan struct{})}
	results := make(chan error, len(stores))
	var started sync.WaitGroup
	started.Add(len(stores))
	for _, store := range stores {
		resolver := newTestProvider(t, store)
		go func() {
			started.Done()
			started.Wait()
			_, err := resolver.ResolveWithRefresh(ctx, driver.Refresh)
			results <- err
		}()
	}
	select {
	case <-driver.entered:
	case <-ctx.Done():
		t.Fatal("refresh was not admitted")
	}
	close(driver.proceed)
	for range stores {
		require.NoError(t, <-results)
	}
	require.EqualValues(t, 1, driver.calls.Load())
	connection, err := stores[0].ReadConnection(ctx, testKey)
	require.NoError(t, err)
	require.EqualValues(t, 3, connection.Revision)
	objects := stores[0].objects
	original, err := objects.CreateObject(ctx, "versions/item", []byte(`{"revision":1}`))
	require.NoError(t, err)
	next, err := objects.CompareAndSwapObject(ctx, "versions/item", original.ETag, []byte(`{"revision":2}`))
	require.NoError(t, err)
	require.NotEqual(t, original.Version, next.Version)
	_, err = objects.CompareAndSwapObject(ctx, "versions/item", original.ETag, []byte(`{"revision":3}`))
	require.ErrorIs(t, err, ErrConflict)
	historical, err := objects.ReadObject(ctx, "versions/item", original.Version)
	require.NoError(t, err)
	require.JSONEq(t, `{"revision":1}`, string(historical.Contents))
	admission, err := stores[0].BeginCredentialExchange(ctx, testKey, connection.Revision, "oauth-crash", time.Now().Add(time.Minute))
	require.NoError(t, err)
	hasResult := false
	fault := &faultObjects{ObjectStore: objects, isAccepted: false, failWrite: func(key string, _ []byte) bool {
		if strings.Contains(key, "/exchange-results/") {
			hasResult = true
		}
		return strings.HasSuffix(key, "/head") && hasResult
	}, failRead: func(key string) bool { return strings.HasSuffix(key, "/head") && hasResult }}
	_, err = newTestStore(t, fault).CommitCredentialExchange(ctx, admission, testMaterial("fixture-recovered", nil))
	require.ErrorIs(t, err, ErrOutcomeUnknown)
	recovered, err := stores[7].RecoverCredentialExchange(ctx, admission)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, recovered.Status)
	material, _, err := stores[4].ReadCredentialMaterial(ctx, testKey)
	require.NoError(t, err)
	var credentials testCredentials
	require.NoError(t, json.Unmarshal(material.Credentials, &credentials))
	require.Equal(t, "fixture-recovered", credentials.Token)
	stale, err := stores[1].BeginCredentialExchange(ctx, testKey, connection.Revision, "oauth-crash", admission.Deadline)
	require.NoError(t, err)
	require.False(t, stale.ProviderDispatchAllowed)
	require.False(t, errors.Is(err, ErrOutcomeUnknown))
	configurations, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: testScope})
	require.NoError(t, err)
	_, _, err = configurations.WriteConfiguration(ctx, 0, Configuration{Connections: []ConnectionConfiguration{}, TriggerBindings: []TriggerConfiguration{}, OperationConfigurations: []OperationConfiguration{}})
	require.NoError(t, err)
	snapshot, err := configurations.FreezeConfiguration(ctx, 1)
	require.NoError(t, err)
	for name, value := range map[string]string{
		"DEX_PROJECT_ID": testScope.ProjectID, "DEX_PROJECT_SCOPE": testScope.Kind, "DEX_PROJECT_SESSION_ID": "",
		"DEX_PROJECT_CONFIG_KEY": snapshot.Key, "DEX_PROJECT_CONFIG_VERSION": snapshot.Version, "DEX_PROJECT_CONFIG_DIGEST": snapshot.Digest,
		"DEX_PROJECT_STORAGE_BUCKET": bucket, "DEX_PROJECT_STORAGE_PREFIX": "replicas", "DEX_PROJECT_STORAGE_KMS_KEY_ARN": "",
		"DEX_PROJECT_ALLOW_LOCAL_STORAGE": "true", "DEX_PROJECT_STORAGE_ENDPOINT": endpoint, "AWS_REGION": "us-east-1",
		"AWS_ACCESS_KEY_ID": accessKey, "AWS_SECRET_ACCESS_KEY": secretKey, "AWS_SESSION_TOKEN": "",
	} {
		t.Setenv(name, value)
	}
	loaded, err := LoadFromEnvironment(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, loaded.Configuration.Revision)
	require.Empty(t, loaded.Configuration.Connections)
	example := exec.CommandContext(ctx, "go", "run", "../examples/projectconfiguration")
	output, err := example.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "Accepted configuration revision 1 with 0 connections")
	t.Setenv("DEX_PROJECT_ID", "different")
	_, err = LoadFromEnvironment(ctx)
	require.Error(t, err, "snapshot cannot be loaded into another project")
	t.Logf("Verified real versioned S3: bucket=%s independent_clients=%d refresh_dispatches=%d historical_version=%s recovered_revision=%d", bucket, len(stores), driver.calls.Load(), original.Version, recovered.Revision)
}

func cleanupFixtureBucket(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	paginator := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		require.NoError(t, err)
		var objects []types.ObjectIdentifier
		for _, version := range page.Versions {
			objects = append(objects, types.ObjectIdentifier{Key: version.Key, VersionId: version.VersionId})
		}
		for _, marker := range page.DeleteMarkers {
			objects = append(objects, types.ObjectIdentifier{Key: marker.Key, VersionId: marker.VersionId})
		}
		if len(objects) > 0 {
			deleted, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket), Delete: &types.Delete{Objects: objects}})
			require.NoError(t, err)
			require.Empty(t, deleted.Errors)
		}
	}
	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
}
