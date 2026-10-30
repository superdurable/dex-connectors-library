// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// The S3 API Reference "Examples: Signature Calculations" key pair, split so no literal looks real.
const (
	exampleAccessKeyID     = "AKIA" + "IOSFODNN7EXAMPLE"
	exampleSecretAccessKey = "wJalrXUtnFEMI/K7MDENG" + "/bPxRfiCYEXAMPLEKEY"
)

var (
	exampleCredentials = Credentials{
		AccessKeyID:     sdkgo.NewSecretString(exampleAccessKeyID),
		SecretAccessKey: sdkgo.NewSecretString(exampleSecretAccessKey),
	}
	exampleSigningTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
)

// requireDocumentedAuthorization allows the signer's ", " separators where the page writes ",".
func requireDocumentedAuthorization(t *testing.T, request *http.Request, signedHeaders string, signature string) {
	t.Helper()
	require.Equal(t, "AWS4-HMAC-SHA256 Credential="+exampleAccessKeyID+"/20130524/us-east-1/s3/aws4_request, SignedHeaders="+
		signedHeaders+", Signature="+signature, request.Header.Get("Authorization"))
}

func TestSignRequestReproducesTheDocumentedGetObjectSignature(t *testing.T) {
	client := newExampleClient(t)
	request := newExampleRequest(t, client, http.MethodGet, objectLocation{bucket: "examplebucket", key: "test.txt", hasKey: true}, nil)
	request.Header.Set("Range", "bytes=0-9")

	require.NoError(t, client.signRequest(request, exampleCredentials, emptyPayloadSHA256, exampleSigningTime))

	require.Equal(t, "https://examplebucket.s3.amazonaws.com/test.txt", request.URL.String())
	requireDocumentedAuthorization(t, request, "host;range;x-amz-content-sha256;x-amz-date",
		"f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41")
}

func TestSignRequestReproducesTheDocumentedPutObjectSignatureForAnEscapedKey(t *testing.T) {
	client := newExampleClient(t)
	request := newExampleRequest(t, client, http.MethodPut, objectLocation{bucket: "examplebucket", key: "test$file.text", hasKey: true}, nil)
	request.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	request.Header.Set("X-Amz-Storage-Class", "REDUCED_REDUNDANCY")

	require.NoError(t, client.signRequest(request, exampleCredentials,
		"44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072", exampleSigningTime))

	require.Equal(t, "/test%24file.text", request.URL.EscapedPath(), "S3 encodes a key once, with uppercase hex")
	requireDocumentedAuthorization(t, request, "date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class",
		"98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd")
}

func TestSignRequestReproducesTheDocumentedListObjectsSignature(t *testing.T) {
	client := newExampleClient(t)
	request := newExampleRequest(t, client, http.MethodGet, objectLocation{bucket: "examplebucket"}, url.Values{"max-keys": {"2"}, "prefix": {"J"}})

	require.NoError(t, client.signRequest(request, exampleCredentials, emptyPayloadSHA256, exampleSigningTime))

	require.Equal(t, "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", request.URL.String())
	requireDocumentedAuthorization(t, request, "host;x-amz-content-sha256;x-amz-date",
		"34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7")
}

func TestSignRequestSignsTheSessionTokenHeader(t *testing.T) {
	client := newExampleClient(t)
	request := newExampleRequest(t, client, http.MethodGet, objectLocation{bucket: "examplebucket", key: "test.txt", hasKey: true}, nil)
	credentials := exampleCredentials
	credentials.SessionToken = sdkgo.NewSecretString("example-session-token")

	require.NoError(t, client.signRequest(request, credentials, emptyPayloadSHA256, exampleSigningTime))

	require.Equal(t, "example-session-token", request.Header.Get("X-Amz-Security-Token"))
	require.Contains(t, request.Header.Get("Authorization"), "SignedHeaders=host;x-amz-content-sha256;x-amz-date;x-amz-security-token,")
}

func TestEscapeS3PathEncodesEveryReservedByteOnce(t *testing.T) {
	require.Equal(t, "/bucket/reports/2026%20Q3/r%C3%A9sum%C3%A9%2B%21%2A%28%29.md", escapeS3Path("/bucket/reports/2026 Q3/résumé+!*().md"))
	require.Equal(t, "/bucket//a~b_c-d.e", escapeS3Path("/bucket//a~b_c-d.e"), "unreserved bytes and empty segments stay")
}

// newExampleClient addresses the documented virtual-hosted examples on the legacy global endpoint.
func newExampleClient(t *testing.T) *Client {
	t.Helper()
	client, err := New(Config{Endpoint: "https://s3.amazonaws.com", Region: "us-east-1", AddressingStyle: AddressingStyleVirtualHosted},
		sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	return client
}

// newExampleRequest builds the unsigned request without a body, as the documented canonical requests sign
// no content-length header.
func newExampleRequest(t *testing.T, client *Client, method string, location objectLocation, query url.Values) *http.Request {
	t.Helper()
	target, err := client.requestURL(location, query)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(context.Background(), method, target.String(), nil)
	require.NoError(t, err)
	request.URL = target
	return request
}
