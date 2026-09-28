// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package providerhttp

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxRetryAfterDelay caps every delay returned by ParseRetryAfter, so a
// provider cannot park a Dex retry for longer than one hour.
const MaxRetryAfterDelay = time.Hour

// ParseRetryAfter converts a Retry-After header value into a retry delay.
//
// It accepts the two RFC 9110 forms: non-negative integer delay-seconds, such
// as "7", and an HTTP-date, which is measured from now. The result is capped at
// MaxRetryAfterDelay. An empty, malformed, zero, or past value returns zero,
// which means the Step's own retry policy applies.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if isASCIIDigits(value) {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seconds >= int64(MaxRetryAfterDelay/time.Second) {
			// Only an overflow fails to parse a string of digits.
			return MaxRetryAfterDelay
		}
		return time.Duration(seconds) * time.Second
	}
	retryAt, err := http.ParseTime(value)
	if err != nil || !retryAt.After(now) {
		return 0
	}
	return min(retryAt.Sub(now), MaxRetryAfterDelay)
}

func isASCIIDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return value != ""
}
