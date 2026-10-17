// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package createcardcheckoutsession

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCardFlowRegistersWithItsStaticConnection(t *testing.T) {
	client, err := stripe.New(stripe.Config{}, sdkgo.StaticCredentialProvider[stripe.Credentials]{})
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, sdkgo.ConnectionRef{Provider: "stripe", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewCardFlow(connection)})
	require.NoError(t, err)
}
