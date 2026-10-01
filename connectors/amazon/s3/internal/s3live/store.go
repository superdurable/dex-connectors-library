// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package s3live reads the S3_CONNECTOR_TEST_* configuration of a live S3 API, such as a local MinIO server,
// and sends the signed bucket-creation and object-deletion requests that live tests need for setup and
// cleanup, which the connector itself does not offer.
package s3live

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/require"
)

const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Store is one live S3 API and a disposable bucket in it.
type Store struct {
	// Endpoint is the custom endpoint; blank means the Amazon S3 Regional endpoint.
	Endpoint string
	// Region is the signing Region.
	Region string
	// AddressingStyle is the connector addressingStyle value; blank means auto.
	AddressingStyle string
	// Bucket is the disposable bucket.
	Bucket string
	// AccessKeyID, SecretAccessKey, and SessionToken sign every request.
	AccessKeyID, SecretAccessKey, SessionToken string
}

// RequireStore reads the live configuration, skipping the test when S3_CONNECTOR_TEST_ACCESS_KEY_ID is unset,
// and creates the bucket when S3_CONNECTOR_TEST_CREATE_BUCKET is true.
func RequireStore(t *testing.T) Store {
	t.Helper()
	store := Store{
		Endpoint: os.Getenv("S3_CONNECTOR_TEST_ENDPOINT"), Region: os.Getenv("S3_CONNECTOR_TEST_REGION"),
		AddressingStyle: os.Getenv("S3_CONNECTOR_TEST_ADDRESSING_STYLE"), Bucket: os.Getenv("S3_CONNECTOR_TEST_BUCKET"),
		AccessKeyID: os.Getenv("S3_CONNECTOR_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("S3_CONNECTOR_TEST_SECRET_ACCESS_KEY"),
		SessionToken: os.Getenv("S3_CONNECTOR_TEST_SESSION_TOKEN"),
	}
	if store.AccessKeyID == "" {
		t.Skip("S3_CONNECTOR_TEST_ACCESS_KEY_ID is not configured")
	}
	require.NotEmpty(t, store.Region, "S3_CONNECTOR_TEST_REGION is the signing Region")
	require.NotEmpty(t, store.Bucket, "S3_CONNECTOR_TEST_BUCKET names a disposable bucket")
	if os.Getenv("S3_CONNECTOR_TEST_CREATE_BUCKET") == "true" {
		store.send(t, http.MethodPut, "", http.StatusOK, http.StatusConflict)
	}
	return store
}

// DeleteAfterTest removes a disposable object when the test ends.
func (store Store) DeleteAfterTest(t *testing.T, key string) {
	t.Cleanup(func() { store.send(t, http.MethodDelete, key, http.StatusNoContent, http.StatusOK) })
}

// send signs one path-style bucket or object request with an empty body.
func (store Store) send(t *testing.T, method string, key string, acceptedStatuses ...int) {
	t.Helper()
	endpoint := store.Endpoint
	if endpoint == "" {
		endpoint = "https://s3." + store.Region + ".amazonaws.com"
	}
	path := "/" + store.Bucket
	if key != "" {
		path += "/" + key
	}
	target, err := url.Parse(endpoint)
	require.NoError(t, err)
	target.Path, target.RawPath = target.Path+path, escapeSignedPath(target.Path+path)
	request, err := http.NewRequestWithContext(context.Background(), method, target.String(), nil)
	require.NoError(t, err)
	request.Header.Set("X-Amz-Content-Sha256", emptyPayloadSHA256)
	credentials := aws.Credentials{AccessKeyID: store.AccessKeyID, SecretAccessKey: store.SecretAccessKey, SessionToken: store.SessionToken}
	signer := v4.NewSigner(func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true })
	require.NoError(t, signer.SignHTTP(context.Background(), credentials, request, emptyPayloadSHA256, "s3", store.Region, time.Now()))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Contains(t, acceptedStatuses, response.StatusCode, "%s %s", method, path)
}

// escapeSignedPath encodes every byte except unreserved characters and slashes, as SigV4 for S3 signs a path.
func escapeSignedPath(path string) string {
	var escaped strings.Builder
	for index := 0; index < len(path); index++ {
		character := path[index]
		if strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~/", character) >= 0 {
			escaped.WriteByte(character)
		} else {
			fmt.Fprintf(&escaped, "%%%02X", character)
		}
	}
	return escaped.String()
}
