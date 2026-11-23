// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * Another invocation owns provider dispatch for this credential generation; retry later.
 */
public final class ExchangePendingException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public ExchangePendingException(final String message) {
        super(message);
    }
}
