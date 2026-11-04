// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceSigningTransport struct{ recorder http.RoundTripper }

func (transport conformanceSigningTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	observed := request.Clone(request.Context())
	observed.Header.Del("X-Amz-Security-Token")
	response, err := transport.recorder.RoundTrip(observed)
	if err == nil {
		if length, parseErr := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64); parseErr == nil {
			response.ContentLength = length
		} else if request.Method == http.MethodHead {
			response.ContentLength = -1
		}
	}
	return response, err
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := s3.New(s3.Config{Endpoint: endpoint, Region: testRegion, DefaultBucket: testBucket}, staticCredentials(""),
		s3.WithHTTPClient(&http.Client{Transport: conformanceSigningTransport{recorder: transport.Transport}}),
		s3.WithClock(func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	context := newDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listObjects":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListObjects(), testConnection),
		"headObject":    newConformanceQueryInvocation(t, scenario.Input, context, client.HeadObject(), testConnection),
		"getObjectText": newConformanceQueryInvocation(t, scenario.Input, context, client.GetObjectText(), testConnection),
		"putObject":     newConformanceMutationInvocation(t, scenario.Input, context, client.PutObject(), testConnection, scenario.CallID),
	}}
}
