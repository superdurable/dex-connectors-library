// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * Transient credential broker transport or service failure; the caller may retry.
 */
public final class CredentialBrokerUnavailableException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     * @param cause the underlying failure, or {@code null}
     */
    public CredentialBrokerUnavailableException(final String message, final Throwable cause) {
        super(message, cause);
    }
}
