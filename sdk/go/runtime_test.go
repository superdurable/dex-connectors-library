// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

func TestCredentialNeverFormatsSecret(t *testing.T) {
	credential := connector.NewCredential(map[string]string{"api_key": "super-secret"})
	require.NotContains(t, fmt.Sprintf("%v", credential), "super-secret")
	require.NotContains(t, fmt.Sprintf("%#v", credential), "super-secret")
}

func TestTypedErrorRetainsCauseAndClassification(t *testing.T) {
	cause := errors.New("dial failed")
	err := connector.NewError(connector.ErrorRetryableAvailability, "mock", "query", "provider unavailable", cause)
	require.True(t, connector.IsRetryable(err))
	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Error(), cause.Error())
}
