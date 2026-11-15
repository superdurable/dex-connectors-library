// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"strings"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// triggerCredentialNamespace prefixes the name-based identity of a Trigger's credential calls.
const triggerCredentialNamespace = "https://superdurable.dev/dex-connectors/trigger-credential/v1"

// NewTriggerCredentialCall returns the stable sdkgo.Call a poller uses to resolve and refresh connection
// credentials outside a Dex Step. Its ID is a UUID v5 (URL namespace) over
// "https://superdurable.dev/dex-connectors/trigger-credential/v1/<provider>/<connection>/<trigger>", so
// every poll of one Trigger on one connection shares an identity across restarts and replicas, as the
// Gmail Trigger's calls do. The call has no Dex context, so it cannot run a Connector Step.
func NewTriggerCredentialCall(connection sdkgo.ConnectionRef, trigger sdkgo.TriggerRef) sdkgo.Call {
	identity := strings.Join([]string{triggerCredentialNamespace, connection.Provider, connection.Name, trigger.TriggerName}, "/")
	return sdkgo.Call{
		ID:         sdkgo.CallID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(identity)).String()),
		Connection: connection,
		Operation:  sdkgo.OperationRef{ConnectorID: trigger.ConnectorID, OperationID: trigger.TriggerName},
	}
}
