// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package profileevent

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/klaviyo"
	"testing"
)

func TestMapToCreateEventInput(t *testing.T) {
	require.Equal(t, klaviyo.CreateEventInput{Email: "member@meridian.co", MetricName: "Signed Up"}, MapToCreateEventInput(Request{Email: "member@meridian.co", MetricName: "Signed Up"}))
}
