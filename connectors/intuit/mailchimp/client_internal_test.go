// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestValidateResolvedCredentialsRequiresAnAPIKey covers the blank key, which no client test resolves.
func TestValidateResolvedCredentialsRequiresAnAPIKey(t *testing.T) {
	_, err := validateResolvedCredentials(Credentials{})
	require.Error(t, err)
	dataCenter, err := validateResolvedCredentials(Credentials{APIKey: sdkgo.NewSecretString("0123456789abcdef" + "0123456789abcdef-us6")})
	require.NoError(t, err)
	require.Equal(t, "us6", dataCenter)
}
