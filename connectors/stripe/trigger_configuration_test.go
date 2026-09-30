// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedTriggerConstraintsMatchProviderAcceptance(t *testing.T) {
	for _, values := range [][]string{nil, {}, {"checkout.session.completed"}, {"checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed", "checkout.session.expired"}, {"charge.succeeded"}, {"checkout.session.completed", "checkout.session.completed"}} {
		configuration := CheckoutSessionUpdatedTriggerConfiguration{EventTypes: values}
		generatedErr := validateCheckoutSessionUpdatedTriggerConfiguration(configuration)
		providerErr := configuration.Validate()
		require.Equal(t, providerErr == nil, generatedErr == nil, "manifest and typed provider admission must agree")
	}
}
