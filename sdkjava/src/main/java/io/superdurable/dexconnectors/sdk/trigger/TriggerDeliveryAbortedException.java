// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Thrown when a Trigger context's cancellation ends delivery.
 */
public final class TriggerDeliveryAbortedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the abort exception with its fixed message.
     */
    public TriggerDeliveryAbortedException() {
        super("Trigger delivery was aborted");
    }
}
