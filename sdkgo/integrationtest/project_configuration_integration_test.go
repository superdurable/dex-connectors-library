//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/integrationtest/fixtureconnector"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

type projectFixtureCredentials struct {
	provider *provider.CredentialProvider[fixtureconnector.Credentials]
	calls    atomic.Int64
}

func (credentials *projectFixtureCredentials) Resolve(call sdkgo.Call) (fixtureconnector.Credentials, error) {
	return sdkgo.ResolveCredential[fixtureconnector.Credentials](call.Context, credentials.provider, call, credentials)
}
func (*projectFixtureCredentials) RefreshRequired(sdkgo.CredentialRefreshState[fixtureconnector.Credentials]) bool {
	return true
}
func (credentials *projectFixtureCredentials) Refresh(_ context.Context, state sdkgo.CredentialRefreshState[fixtureconnector.Credentials]) (sdkgo.CredentialRefreshResult[fixtureconnector.Credentials], error) {
	credentials.calls.Add(1)
	return sdkgo.CredentialRefreshResult[fixtureconnector.Credentials]{Credentials: fixtureconnector.Credentials{Token: sdkgo.NewSecretString("fixture-rotated-private")}, ExpiresAt: state.Now.Add(time.Hour)}, nil
}

func TestProjectConfigurationCredentialBoundaryWithRealDexAndS3(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	endpoint := os.Getenv("PROJECTCONFIG_TEST_S3_ENDPOINT")
	require.Equal(t, "http://127.0.0.1:29000", endpoint, "this integration uses only the isolated local Kind fixture")
	accessKey, secretKey := os.Getenv("PROJECTCONFIG_TEST_S3_ACCESS_KEY"), os.Getenv("PROJECTCONFIG_TEST_S3_SECRET_KEY")
	require.NotEmpty(t, accessKey)
	require.NotEmpty(t, secretKey)
	s3Client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey}, nil
	})})
	bucket := fmt.Sprintf("sv2-sdk-dex-%d", time.Now().UnixNano())
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProjectConfigurationBucket(t, s3Client, bucket) })
	_, err = s3Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}})
	require.NoError(t, err)
	objects, err := projectconfig.NewS3ObjectStore(ctx, &projectconfig.S3StoreConfig{Client: s3Client, Bucket: bucket, AllowUnencrypted: true})
	require.NoError(t, err)
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: objects, Scope: projectconfig.Scope{ProjectID: "fixture", Kind: "live"}})
	require.NoError(t, err)
	key := projectconfig.ConnectionKey{ConnectorID: "fixture", ConnectionName: "integration"}
	expiry := time.Now().Add(-time.Minute)
	_, err = store.ReplaceCredential(ctx, key, 0, projectconfig.CredentialMaterial{Credentials: json.RawMessage(`{"token":"fixture-old-private"}`), ExpiresAt: &expiry, ModuleVersion: "v0.1.0", AuthMethod: "fixture"})
	require.NoError(t, err)
	typed, err := provider.NewCredentialProvider(&provider.Config[fixtureconnector.Credentials]{Store: store, Key: key, Decode: decodeProjectFixtureCredentials, Encode: encodeProjectFixtureCredentials})
	require.NoError(t, err)
	credentials := &projectFixtureCredentials{provider: typed}
	fixture := fixtureconnector.NewProvider()
	connection, err := fixtureconnector.NewConnectionWithCredentialProvider(fixture, "integration", credentials)
	require.NoError(t, err)
	flow := connectorConsumerFlow{connection: connection}
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cache.Close()) })
	port := availablePort(t)
	target := net.JoinHostPort(environmentOr("DEX_WORKER_ADVERTISED_HOST", "host.docker.internal"), port)
	dexAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:28811")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: net.JoinHostPort("0.0.0.0", port), FlowServiceAddress: dexAddress, WorkerTarget: dex.WorkerTarget{Address: target}})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, worker.Stop(stopCtx))
		require.NoError(t, <-workerResult)
	})
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress, WorkerTarget: &dex.WorkerTarget{Address: target}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	flowID := fmt.Sprintf("sdk-project-configuration-%d", time.Now().UnixNano())
	runID, err := client.StartFlow(ctx, flow, flowID, flowInput{Name: "project-config-widget"}, dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output flowOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "project-config-widget", output.Widget.Name)
	require.EqualValues(t, 1, credentials.calls.Load())
	require.Len(t, fixture.Stats().MutationCalls, 2, "provider mutation retry reuses already rotated credentials")
	metadata, err := store.ReadConnection(ctx, key)
	require.NoError(t, err)
	require.Equal(t, projectconfig.CredentialReady, metadata.Status)
	require.EqualValues(t, 3, metadata.Revision)
	t.Logf("Real Dex/S3 credential boundary: flow=%s run=%v credential_revision=%d refresh_calls=%d", flowID, runID, metadata.Revision, credentials.calls.Load())
}
func decodeProjectFixtureCredentials(contents json.RawMessage) (fixtureconnector.Credentials, error) {
	var value struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(contents, &value); err != nil {
		return fixtureconnector.Credentials{}, err
	}
	return fixtureconnector.Credentials{Token: sdkgo.NewSecretString(value.Token)}, nil
}
func encodeProjectFixtureCredentials(credentials fixtureconnector.Credentials) (json.RawMessage, error) {
	return json.Marshal(struct {
		Token string `json:"token"`
	}{Token: credentials.Token.Reveal()})
}
func cleanupProjectConfigurationBucket(t *testing.T, client *s3.Client, bucket string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	page, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	require.False(t, aws.ToBool(page.IsTruncated))
	var objects []types.ObjectIdentifier
	for _, version := range page.Versions {
		objects = append(objects, types.ObjectIdentifier{Key: version.Key, VersionId: version.VersionId})
	}
	if len(objects) > 0 {
		result, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket), Delete: &types.Delete{Objects: objects}})
		require.NoError(t, err)
		require.Empty(t, result.Errors)
	}
	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
}
