// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsRefreshRequired(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	insideSkew := now.Add(RefreshSkew)
	beyondSkew := insideSkew.Add(time.Nanosecond)
	for _, test := range []struct {
		name              string
		hasAccessToken    bool
		expiresAt         *time.Time
		rule              MissingExpiryRule
		isRefreshRequired bool
	}{
		{name: "absent access token", expiresAt: &beyondSkew, rule: KeepWhenExpiryMissing, isRefreshRequired: true},
		{name: "missing expiry refreshes", hasAccessToken: true, rule: RefreshWhenExpiryMissing, isRefreshRequired: true},
		{name: "missing expiry keeps non-expiring token", hasAccessToken: true, rule: KeepWhenExpiryMissing},
		{name: "expiry inside skew", hasAccessToken: true, expiresAt: &insideSkew, rule: KeepWhenExpiryMissing, isRefreshRequired: true},
		{name: "expiry beyond skew", hasAccessToken: true, expiresAt: &beyondSkew, rule: RefreshWhenExpiryMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.isRefreshRequired, IsRefreshRequired(test.hasAccessToken, test.expiresAt, now, test.rule))
		})
	}
}
