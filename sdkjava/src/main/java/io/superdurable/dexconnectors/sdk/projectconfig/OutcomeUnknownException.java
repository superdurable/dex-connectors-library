// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * A conditional write may have reached storage; reconcile by reading back before any retry.
 */
public final class OutcomeUnknownException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public OutcomeUnknownException(final String message) {
        super(message);
    }
}
