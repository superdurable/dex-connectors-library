// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alibaba

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

var conformanceRequestDate = time.Date(2023, 10, 26, 10, 22, 32, 0, time.UTC)
var conformanceNonceBytes = []byte{0x31, 0x56, 0x85, 0x32, 0x99, 0xf3, 0x13, 0xe2, 0x3d, 0xc1, 0x2e, 0x17, 0x03, 0xdc, 0x12, 0xe1}

type conformanceSigningOption struct{}

func (conformanceSigningOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return conformanceRequestDate }
	client.nonceSource = bytes.NewReader(conformanceNonceBytes)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "alibaba", Name: "alibaba"}
	accessKeyID := "test-access-key-id"
	if scenario.Name == "describeRegions-defect" {
		accessKeyID = "bad key"
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "describeRegions-oversized" {
		config.MaxResponseBytes = 4
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessKeyID: sdkgo.NewSecretString(accessKeyID), AccessKeySecret: sdkgo.NewSecretString("test-access-key-secret")},
	}, WithHTTPClient(transport), conformanceSigningOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"describeRegions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.DescribeRegions(), reference),
	}}
}
