// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
)

func TestDecodeResolvedCredentialsJSON(t *testing.T) {
	credentials, err := stripe.DecodeResolvedCredentialsJSON([]byte(
		`{"secret_key":"sk_test_example","webhook_secret":"whsec_example"}`,
	))
	require.NoError(t, err)
	require.Equal(t, "sk_test_example", credentials.SecretKey.Reveal())
	require.Equal(t, "whsec_example", credentials.WebhookSecret.Reveal())

	_, err = stripe.DecodeResolvedCredentialsJSON([]byte(
		`{"secret_key":"sk_test_example","webhook_secret":"whsec_example","extra":"rejected"}`,
	))
	require.Error(t, err)
}
