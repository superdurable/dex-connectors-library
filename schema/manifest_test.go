// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestDecodeManifest(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata:
  name: mock-provider
  displayName: Mock Provider
  version: v0.1.0-alpha.1
  description: deterministic test provider
spec:
  provider: mock
  auth: {type: none, fields: []}
  operations:
    - {name: getProfile, kind: query, description: read profile, idempotency: none}
    - {name: grantCredit, kind: action, description: grant credit, idempotency: required}
`))
	require.NoError(t, err)
	require.Equal(t, "mock-provider", manifest.Metadata.Name)
}

func TestRejectActionWithoutIdempotency(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, version: v0.1.0, description: bad}
spec:
  provider: bad
  auth: {type: none, fields: []}
  operations:
    - {name: mutateThing, kind: action, description: unsafe, idempotency: none}
`))
	require.ErrorContains(t, err, "actions must declare")
}
