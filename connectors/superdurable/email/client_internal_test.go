// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestValidateResolvedCredentialsRequiresAPassword covers a missing password, which no client test resolves.
func TestValidateResolvedCredentialsRequiresAPassword(t *testing.T) {
	require.Error(t, validateResolvedCredentials(Credentials{Username: "support@example.com"}))
	require.NoError(t, validateResolvedCredentials(Credentials{
		Username: "support@example.com", Password: sdkgo.NewSecretString("app-password"), SMTPUsername: "relay",
	}))
}
