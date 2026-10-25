// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenAICompatibleAddressPolicy checks the resolved addresses before any credentialed dial.
func TestOpenAICompatibleAddressPolicy(t *testing.T) {
	for _, testCase := range []struct {
		address      string
		loopbackHost bool
		allowed      bool
	}{
		{"8.8.8.8", false, true},
		{"127.0.0.1", false, false},
		{"127.0.0.1", true, true},
		{"10.0.0.1", false, false},
		{"192.168.1.1", false, false},
		{"169.254.169.254", false, false},
		{"100.64.0.1", false, false},
		{"fd00:ec2::254", false, false},
		{"fe80::1", false, false},
		{"::1", true, true},
		{"::ffff:169.254.169.254", false, false},
	} {
		t.Run(testCase.address, func(t *testing.T) {
			address := netip.MustParseAddr(testCase.address).Unmap()
			require.Equal(t, testCase.allowed, isAllowedOpenAICompatibleAddress(address, testCase.loopbackHost))
		})
	}
}
