// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * The credential broker does not relay this shared connection's provider events.
 */
public final class RelayedTriggerNotRelayedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     */
    public RelayedTriggerNotRelayedException() {
        super("the credential broker does not relay this connection's events");
    }
}
