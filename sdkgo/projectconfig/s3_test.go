// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

func TestS3BoundaryRequiresVersionAndKMSAndDisablesWriteRetry(t *testing.T) {
	const keyARN = "arn:aws:kms:us-east-1:123456789012:key/00000000-0000-0000-0000-000000000000"
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Has("versioning") {
			writer.Header().Set("Content-Type", "application/xml")
			_, err := io.WriteString(writer, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
			require.NoError(t, err)
			return
		}
		requests.Add(1)
		require.Equal(t, "/bucket/environment/secret/head", request.URL.Path)
		if request.Method == http.MethodPut {
			require.Equal(t, "aws:kms", request.Header.Get("X-Amz-Server-Side-Encryption"))
			require.Equal(t, keyARN, request.Header.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id"))
			require.Equal(t, `"original"`, request.Header.Get("If-Match"))
			require.Empty(t, request.Header.Get("If-None-Match"))
			writer.WriteHeader(http.StatusServiceUnavailable)
			require.NoError(t, xml.NewEncoder(writer).Encode(struct {
				XMLName xml.Name `xml:"Error"`
				Code    string   `xml:"Code"`
			}{Code: "SlowDown"}))
			return
		}
		require.Equal(t, "requested", request.URL.Query().Get("versionId"))
		writer.Header().Set("X-Amz-Version-Id", "requested")
		writer.Header().Set("ETag", `"etag"`)
		writer.Header().Set("X-Amz-Server-Side-Encryption", "aws:kms")
		writer.Header().Set("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id", keyARN)
		_, err := io.WriteString(writer, `{"private":"value"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(server.URL), UsePathStyle: true, Credentials: aws.AnonymousCredentials{}})
	_, err := NewS3ObjectStore(context.Background(), &S3StoreConfig{Client: client, Bucket: "bucket", KMSKeyID: "alias/unsafe"})
	require.Error(t, err)
	store, err := NewS3ObjectStore(context.Background(), &S3StoreConfig{Client: client, Bucket: "bucket", Prefix: "environment", KMSKeyID: keyARN})
	require.NoError(t, err)
	_, err = store.CompareAndSwapObject(context.Background(), "secret/head", `"original"`, []byte(`{"value":1}`))
	require.ErrorIs(t, err, ErrOutcomeUnknown)
	require.EqualValues(t, 1, requests.Load(), "provider admission writes cannot be invisibly retried")
	object, err := store.ReadObject(context.Background(), "secret/head", "requested")
	require.NoError(t, err)
	require.Equal(t, "requested", object.Version)
}

func TestS3ReadRejectsMissingVersionOrEncryption(t *testing.T) {
	for _, kind := range []string{"version", "encryption", "different-version"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Query().Has("versioning") {
					_, err := io.WriteString(writer, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
					require.NoError(t, err)
					return
				}
				writer.Header().Set("ETag", `"etag"`)
				if kind != "version" {
					writer.Header().Set("X-Amz-Version-Id", "wrong")
				}
				_, err := io.WriteString(writer, `{}`)
				require.NoError(t, err)
			}))
			defer server.Close()
			client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(server.URL), UsePathStyle: true, Credentials: aws.AnonymousCredentials{}})
			store, err := NewS3ObjectStore(context.Background(), &S3StoreConfig{Client: client, Bucket: "bucket", KMSKeyID: "arn:aws:kms:us-east-1:123456789012:key/example"})
			require.NoError(t, err)
			version := ""
			if kind == "different-version" {
				version = "expected"
			}
			_, err = store.ReadObject(context.Background(), "secret/head", version)
			require.Error(t, err)
		})
	}
}

func TestS3OperationAbortedIsAConditionalConflict(t *testing.T) {
	require.ErrorIs(t, classifyStorageError(&smithy.GenericAPIError{Code: "OperationAborted", Message: "A conflicting conditional operation is currently in progress"}, true), ErrConflict)
}
