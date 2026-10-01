// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestNewDerivesTheRegionalEndpointAndRejectsUnsafeConfiguration(t *testing.T) {
	credentials := staticCredentials("")
	for _, region := range []string{"us-east-1", "eu-west-1", "us-gov-west-1", "cn-north-1", "eusc-de-east-1"} {
		_, err := s3.New(s3.Config{Region: region}, credentials)
		require.NoError(t, err, region)
	}
	for name, config := range map[string]s3.Config{
		"an R2 Region without an endpoint":   {Region: "auto"},
		"a Region that is not a Region code": {Region: "eu-west-1/s3"},
		"a plain HTTP remote endpoint":       {Endpoint: "http://minio.example.com", Region: "us-east-1"},
		"an endpoint with user information":  {Endpoint: "https://user:secret@minio.example.com", Region: "us-east-1"},
		"an endpoint with a query":           {Endpoint: "https://minio.example.com?x=1", Region: "us-east-1"},
		"an endpoint path needing escapes":   {Endpoint: "https://gateway.example.com/s3%20api", Region: "us-east-1"},
		"an unknown addressing style":        {AddressingStyle: "dns"},
		"an invalid default bucket":          {DefaultBucket: "Acme_Reports"},
		"a page size above the S3 limit":     {ListPageSize: 1001},
		"an upload limit above 5 MiB":        {MaxUploadBytes: 5<<20 + 1},
	} {
		_, err := s3.New(config, credentials)
		require.Error(t, err, name)
	}
	_, err := s3.New(s3.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
}

func TestDefaultsKeepTheManifestValues(t *testing.T) {
	require.Equal(t, s3.Config{
		Region: "us-east-1", AddressingStyle: s3.AddressingStyleAuto, ListPageSize: 100,
		MaxResponseBytes: 4 << 20, MaxTextBytes: 1 << 20, MaxUploadBytes: 1 << 20,
	}, s3.DefaultConfig())
}

func TestAutoAddressingUsesPathStyleForACustomEndpointAndVirtualHostsOnlyWhenRequested(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/a.txt", s3fake.Object{Body: []byte("a"), ContentType: "text/plain"})

	pathClient := newPathStyleClient(t, store)
	result, err := runQuery(t, "path-style", pathClient.HeadObject(), s3.HeadObjectInput{Key: "reports/a.txt"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchFound, result.Branch)
	require.Equal(t, "/"+testBucket+"/reports/a.txt", store.Requests()[0].EscapedPath)
	require.Equal(t, store.HostPort(), store.Requests()[0].Host)

	virtualClient, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, AddressingStyle: s3.AddressingStyleVirtualHosted},
		staticCredentials(""), s3.WithHTTPClient(store.VirtualHostClient()))
	require.NoError(t, err)
	result, err = runQuery(t, "virtual-host", virtualClient.HeadObject(), s3.HeadObjectInput{Bucket: testBucket, Key: "reports/a.txt"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchFound, result.Branch)
	require.Equal(t, "/reports/a.txt", store.Requests()[1].EscapedPath)
	require.Equal(t, testBucket+"."+store.HostPort(), store.Requests()[1].Host)
	require.Zero(t, store.SignatureErrorCount())
}

func TestEveryRequestIsSignedForTheConfiguredRegionAndNeverCarriesTheSecret(t *testing.T) {
	store := newFakeStore(t, func(config *s3fake.Config) { config.SessionToken = testSessionToken })
	client, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, DefaultBucket: testBucket}, staticCredentials(testSessionToken))
	require.NoError(t, err)
	context := newDexContext("signed-put")
	_, err = runMutation(t, context, client.PutObject(), s3.PutObjectInput{Key: "a b+c/é.txt", ContentType: "text/plain", TextContent: "hello"})
	require.NoError(t, err)
	_, err = runQuery(t, "signed-list", client.ListObjects(), s3.ListObjectsInput{Prefix: "a b+c/", Delimiter: "/"})
	require.NoError(t, err)

	require.Zero(t, store.SignatureErrorCount(), "the fake recomputed every signature from the request it received")
	for _, request := range store.Requests() {
		require.Contains(t, request.Header.Get("Authorization"), "Credential="+testAccessKeyID+"/")
		require.Contains(t, request.Header.Get("Authorization"), "/"+testRegion+"/s3/aws4_request")
		require.Equal(t, testSessionToken, request.Header.Get("X-Amz-Security-Token"))
		require.Equal(t, "identity", request.Header.Get("Accept-Encoding"))
		for name, values := range request.Header {
			require.NotContains(t, strings.Join(values, ","), testSecretAccessKey, name)
		}
		require.NotContains(t, request.EscapedPath+fmt.Sprint(request.Query), testSecretAccessKey)
	}
	require.Equal(t, "/"+testBucket+"/a%20b%2Bc/%C3%A9.txt", store.Requests()[0].EscapedPath)
}

func TestMissingOrUnsafeCredentialsSelectDefectWithoutARequest(t *testing.T) {
	store := newFakeStore(t)
	for name, credentials := range map[string]s3.Credentials{
		"no access key ID":          {SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey)},
		"an access key ID with /":   {AccessKeyID: sdkgo.NewSecretString("AKIA/EXAMPLE"), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey)},
		"no secret access key":      {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID)},
		"a session token with a CR": {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey), SessionToken: sdkgo.NewSecretString("token\r\nX: y")},
	} {
		client, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, DefaultBucket: testBucket},
			sdkgo.StaticCredentialProvider[s3.Credentials]{testConnection: credentials})
		require.NoError(t, err)
		result, err := runQuery(t, "missing-credentials", client.HeadObject(), s3.HeadObjectInput{Key: "a.txt"})
		require.NoError(t, err, name)
		require.Equal(t, s3.HeadObjectBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
	}
	require.Zero(t, store.RequestCount(""))
}

func TestCredentialRejectionsNameTheS3CodeButNeverItsMessage(t *testing.T) {
	store := newFakeStore(t)
	client, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, DefaultBucket: testBucket},
		sdkgo.StaticCredentialProvider[s3.Credentials]{testConnection: {
			AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString("a-different-secret"),
		}})
	require.NoError(t, err)
	result, err := runQuery(t, "bad-secret", client.GetObjectText(), s3.GetObjectTextInput{Key: "a.txt"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "HTTP 403 SignatureDoesNotMatch")
	requireNoSecrets(t, result)
}

func TestWrongRegionNamesTheBucketRegionS3Reports(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(s3fake.Request) *s3fake.Response {
		return &s3fake.Response{StatusCode: http.StatusMovedPermanently, Code: "PermanentRedirect", Header: http.Header{"X-Amz-Bucket-Region": {"ap-southeast-2"}}}
	})
	result, err := runQuery(t, "wrong-region", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "the bucket is in ap-southeast-2, so set region to ap-southeast-2")
	requireNoSecrets(t, result)
}

func TestRetryableStatusesRetryAndConclusiveOnesSelectProviderRejected(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{http.StatusServiceUnavailable, "SlowDown", sdkgo.FailureRateLimit, true},
		{http.StatusInternalServerError, "InternalError", sdkgo.FailureAvailability, true},
		{http.StatusBadRequest, "RequestTimeout", sdkgo.FailureTransport, true},
		{http.StatusTooManyRequests, "", sdkgo.FailureRateLimit, true},
		{http.StatusTemporaryRedirect, "TemporaryRedirect", sdkgo.FailureAvailability, true},
		{http.StatusForbidden, "AccessDenied", sdkgo.FailureAuthorization, false},
		{http.StatusForbidden, "RequestTimeTooSkewed", sdkgo.FailureAuthentication, false},
		{http.StatusBadRequest, "ExpiredToken", sdkgo.FailureAuthentication, false},
		{http.StatusNotFound, "NoSuchBucket", sdkgo.FailureNotFound, false},
		{http.StatusNotImplemented, "NotImplemented", sdkgo.FailureProviderRejection, false},
		{http.StatusBadRequest, "InvalidArgument", sdkgo.FailureProviderRejection, false},
	} {
		store := newFakeStore(t)
		store.Intercept(func(s3fake.Request) *s3fake.Response {
			return &s3fake.Response{StatusCode: test.status, Code: test.code}
		})
		result, err := runQuery(t, "status", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{})
		name := fmt.Sprintf("%d %s", test.status, test.code)
		if test.retry {
			requireRetry(t, err, test.kind)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, s3.ListObjectsBranchProviderRejected, result.Branch, name)
		require.Equal(t, test.kind, result.Failure.Kind, name)
		requireNoSecrets(t, result)
	}
}

func TestAnErrorCodeThatRepeatsACredentialIsNeverReported(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(s3fake.Request) *s3fake.Response {
		return &s3fake.Response{StatusCode: http.StatusForbidden, Code: testAccessKeyID}
	})
	result, err := runQuery(t, "reflected-code", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, result.Branch)
	require.Equal(t, "S3 denied the list with HTTP 403", result.Failure.Message)
	requireNoSecrets(t, result)
}

func TestDecodeResolvedCredentialsJSONAcceptsOnlyTheDeclaredFields(t *testing.T) {
	credentials, err := s3.DecodeResolvedCredentialsJSON(json.RawMessage(
		`{"access_key_id":"` + testAccessKeyID + `","secret_access_key":"` + testSecretAccessKey + `","session_token":"` + testSessionToken + `"}`))
	require.NoError(t, err)
	require.Equal(t, testSessionToken, credentials.SessionToken.Reveal())
	_, err = s3.DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_key_id":"` + testAccessKeyID + `","secret_access_key":"x","region":"us-east-1"}`))
	require.Error(t, err)
	require.NotContains(t, err.Error(), testAccessKeyID)
	_, err = s3.DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_key_id":"` + testAccessKeyID + `"}`))
	require.Error(t, err)
}

func TestConnectionCannotBeSerialized(t *testing.T) {
	client, err := s3.New(s3.Config{}, staticCredentials(""))
	require.NoError(t, err)
	connection, err := s3.NewConnection(client, testConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "s3.Connection{[REDACTED]}", fmt.Sprint(connection))
}
