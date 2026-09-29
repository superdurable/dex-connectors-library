// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
)

func TestDecodeResolvedCredentialsJSON(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		contents      string
		secretKey     string
		webhookSecret string
	}{
		{name: "operation", contents: `{"secret_key":"sk_test_example"}`, secretKey: "sk_test_example"},
		{name: "webhook", contents: `{"webhook_secret":"whsec_example"}`, webhookSecret: "whsec_example"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			credentials, err := stripe.DecodeResolvedCredentialsJSON([]byte(fixture.contents))
			require.NoError(t, err)
			require.Equal(t, fixture.secretKey, credentials.SecretKey.Reveal())
			require.Equal(t, fixture.webhookSecret, credentials.WebhookSecret.Reveal())
		})
	}

	_, err := stripe.DecodeResolvedCredentialsJSON([]byte(
		`{"secret_key":"sk_test_example","webhook_secret":"whsec_example","extra":"rejected"}`,
	))
	require.Error(t, err)
	_, err = stripe.DecodeResolvedCredentialsJSON([]byte(`{}`))
	require.Error(t, err)
}
