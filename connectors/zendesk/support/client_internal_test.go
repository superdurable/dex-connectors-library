// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestValidateResolvedCredentialsRequiresAnEmail covers the missing agent email, which no client test resolves.
func TestValidateResolvedCredentialsRequiresAnEmail(t *testing.T) {
	token := sdkgo.NewSecretString("zendeskTestToken0123456789abcdef")
	require.Error(t, validateResolvedCredentials(Credentials{APIToken: token}))
	require.NoError(t, validateResolvedCredentials(Credentials{Email: "agent@acme.example.com", APIToken: token}))
}
