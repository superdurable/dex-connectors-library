// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseProtoDurationAcceptsOnlyPositiveSeconds(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"37s": 37 * time.Second, "0.25s": 250 * time.Millisecond, "1.000000001s": time.Second + time.Nanosecond,
		"7200s": maxProviderRetryDelay, "0s": 0, "-1s": 0, "1m": 0, "1h": 0, "37": 0, "": 0, "abc": 0,
		"99999999999s": 0,
	} {
		require.Equal(t, want, parseProtoDuration(value), value)
	}
}

func TestRetryAfterDelayAcceptsSecondsAndHTTPDates(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for value, want := range map[string]time.Duration{
		"3":                             3 * time.Second,
		" 10 ":                          10 * time.Second,
		"0":                             0,
		"-4":                            0,
		"999999":                        maxProviderRetryDelay,
		"Sat, 26 Sep 2026 12:00:30 GMT": 30 * time.Second,
		"Sat, 26 Sep 2026 11:59:00 GMT": 0,
		"Sat, 26 Sep 2026 18:00:00 GMT": maxProviderRetryDelay,
		"later":                         0,
		"":                              0,
	} {
		header := http.Header{}
		if value != "" {
			header.Set("Retry-After", value)
		}
		require.Equal(t, want, retryAfterDelay(header, now), value)
	}
}
