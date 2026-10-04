// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	reportarchive "github.com/superdurable/dex-connectors-library/connectors/amazon/s3/examples/report-archive/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("AMAZON_S3_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("AMAZON_S3_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("AMAZON_S3_EXAMPLE_MISSING", "fallback"))
}

// TestTheDexWebConnectionSettingsLoad proves the settings Dex Web saves for this example build a Connection.
func TestTheDexWebConnectionSettingsLoad(t *testing.T) {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: "amazon-s3", ConnectionName: reportarchive.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/amazon/s3", Provider: "amazon-s3",
		Configuration: json.RawMessage(`{"region": "us-east-1", "endpoint": "http://127.0.0.1:9000", "defaultBucket": "acme-reports"}`),
	}}}
	var config s3.Config
	require.NoError(t, configuration.DecodeConnectionConfiguration(
		projectconfig.ConnectionKey{ConnectorID: s3.ConnectorID, ConnectionName: reportarchive.ConnectionName}, &config))
	client, err := s3.New(config, sdkgo.StaticCredentialProvider[s3.Credentials]{})
	require.NoError(t, err)
	_, err = s3.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-s3", Name: reportarchive.ConnectionName})
	require.NoError(t, err)
}
