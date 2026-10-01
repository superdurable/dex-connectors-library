// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	reportarchive "github.com/superdurable/dex-connectors-library/connectors/amazon/s3/examples/report-archive/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("AMAZON_S3_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("AMAZON_S3_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("AMAZON_S3_EXAMPLE_MISSING", "fallback"))
}

// TestTheDexWebConnectionRecordLoads proves the record Dex Web writes for this example builds a Connection.
func TestTheDexWebConnectionRecordLoads(t *testing.T) {
	connectionsPath := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "amazon-s3",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/amazon/s3",
    "moduleVersion": "v0.1.0",
    "provider": "amazon-s3",
    "connectionName": "`+reportarchive.ConnectionName+`",
    "configuration": {"region": "us-east-1", "endpoint": "http://127.0.0.1:9000", "defaultBucket": "acme-reports"},
    "credentials": {"access_key_id": "example-access-key-id", "secret_access_key": "example-secret-access-key"}
  }]
}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	_, err = s3.NewLocalConnection(store, reportarchive.ConnectionName)
	require.NoError(t, err)
}
