// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	queueconsumer "github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/examples/queue-consumer/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("AMAZON_SQS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("AMAZON_SQS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("AMAZON_SQS_EXAMPLE_MISSING", "fallback"))
}

// TestTheDexWebConnectionSettingsLoad proves the settings Dex Web saves for this example build a Connection.
func TestTheDexWebConnectionSettingsLoad(t *testing.T) {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: "amazon-sqs", ConnectionName: queueconsumer.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/amazon/sqs", Provider: "amazon-sqs",
		Configuration: json.RawMessage(`{"region": "eu-west-1", "defaultQueueUrl": "https://sqs.eu-west-1.amazonaws.com/123456789012/orders", "receiveWaitSeconds": 10}`),
	}}}
	var config sqs.Config
	require.NoError(t, configuration.DecodeConnectionConfiguration(
		projectconfig.ConnectionKey{ConnectorID: sqs.ConnectorID, ConnectionName: queueconsumer.ConnectionName}, &config))
	require.Equal(t, int64(10), config.ReceiveWaitSeconds)
	client, err := sqs.New(config, sdkgo.StaticCredentialProvider[sqs.Credentials]{})
	require.NoError(t, err)
	_, err = sqs.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: queueconsumer.ConnectionName})
	require.NoError(t, err)
}
