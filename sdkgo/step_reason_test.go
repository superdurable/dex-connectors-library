// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestUnwiredBranchFailureReasonBoundsInvalidUTF8(t *testing.T) {
	identity := connectorStepIdentity{
		operation:  OperationRef{ConnectorID: "fixture", OperationID: "selectDefect"},
		connection: ConnectionRef{Provider: "fixture", Name: "test-connection"},
	}
	tests := map[string]string{
		"continuation bytes": string(bytes.Repeat([]byte{0x80}, 1025)),
		"broken sequence":    strings.Repeat("a", 1023) + string([]byte{0xc3}) + "x",
	}
	for name, message := range tests {
		t.Run(name, func(t *testing.T) {
			reason := unwiredBranchFailureReason(identity, DefectBranchID, &Failure{
				Kind: FailureProtocol, Message: message,
			})
			prefix := "fixture selectDefect (connection test-connection) selected branch defect with no target: PROTOCOL: "
			require.True(t, utf8.ValidString(reason))
			require.True(t, strings.HasPrefix(reason, prefix))
			require.True(t, strings.HasSuffix(reason, " (truncated)"))
			boundedMessage := strings.TrimSuffix(strings.TrimPrefix(reason, prefix), " (truncated)")
			require.LessOrEqual(t, len(boundedMessage), 1024)
		})
	}
}

func TestUnwiredBranchFailureReasonReplacesInvalidUTF8(t *testing.T) {
	identity := connectorStepIdentity{
		operation:  OperationRef{ConnectorID: "fixture", OperationID: "selectDefect"},
		connection: ConnectionRef{Provider: "fixture", Name: "test-connection"},
	}
	prefix := "fixture selectDefect (connection test-connection) selected branch defect with no target: PROTOCOL: "
	tests := map[string]struct {
		message  string
		expected string
	}{
		"stray byte":         {message: "a\xffb", expected: "a�b"},
		"truncated sequence": {message: "ok\xc3", expected: "ok�"},
		"valid text":         {message: "café", expected: "café"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			reason := unwiredBranchFailureReason(identity, DefectBranchID, &Failure{Kind: FailureProtocol, Message: test.message})

			require.Equal(t, prefix+test.expected, reason, "a message within the bound is not marked truncated")
			require.True(t, utf8.ValidString(reason))
		})
	}
}
