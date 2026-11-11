// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// connectionTarget is what one project connection points at.
type connectionTarget struct {
	// fingerprint is the connection's projectconfig ConnectionTargetFingerprint.
	fingerprint string
	// isVerifiable reports whether credential resolution can require fingerprint:
	// only a Dex Web connections file record that is not a shared entry can.
	isVerifiable bool
}

// connectionTargetOf returns the target of one project connection, or the
// reason the connection is skipped.
func connectionTargetOf(snapshot projectSnapshot, connectorID string, connectionName string) (connectionTarget, string) {
	if snapshot.project == nil {
		return connectionTarget{}, "it has no configuration"
	}
	for _, configuration := range snapshot.project.Configuration.Connections {
		if configuration.ConnectorID != connectorID || configuration.ConnectionName != connectionName {
			continue
		}
		fingerprint, err := configuration.ConnectionTargetFingerprint()
		if err != nil {
			return connectionTarget{}, "its settings cannot be fingerprinted: " + err.Error()
		}
		return connectionTarget{
			fingerprint:  fingerprint,
			isVerifiable: snapshot.canVerifyConnectionTargets && configuration.SharedConnectionID == "",
		}, ""
	}
	return connectionTarget{}, "it has no configuration"
}

// connectionKeyOf names the connection whose credentials an attempt resolves.
func connectionKeyOf(connectorID string, connectionName string) projectconfig.ConnectionKey {
	return projectconfig.ConnectionKey{ConnectorID: connectorID, ConnectionName: connectionName}
}
