// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.pollingtrigger;

import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.trigger.TriggerRef;

/**
 * The stable credential Call identity of a polling Trigger.
 */
public final class TriggerCredentialCalls {
    private static final String TRIGGER_CREDENTIAL_NAMESPACE =
            "https://superdurable.dev/dex-connectors/trigger-credential/v1";

    private TriggerCredentialCalls() {
    }

    /**
     * Returns the stable Call a poller uses to resolve and refresh connection credentials outside a Step.
     *
     * <p>Its ID is the Go SDK's: a UUIDv5 in the URL namespace over
     * {@code https://superdurable.dev/dex-connectors/trigger-credential/v1/<provider>/<connection>/<trigger>},
     * so every poll of one Trigger on one connection shares an identity across restarts, replicas,
     * and languages. The Call has no Dex Context, so it cannot run a Connector Step.
     *
     * @param connection the logical connection
     * @param trigger the Trigger identity
     * @return the credential Call
     * @throws IllegalArgumentException for an invalid reference
     */
    public static Call newTriggerCredentialCall(final ConnectionRef connection, final TriggerRef trigger) {
        final String identity = TRIGGER_CREDENTIAL_NAMESPACE + "/" + connection.getProvider() + "/"
                + connection.getName() + "/" + trigger.getTriggerName();
        return Call.newTriggerCall(connection, new OperationRef(trigger.getConnectorId(), trigger.getTriggerName()),
                CallId.fromUrlName(identity));
    }
}
