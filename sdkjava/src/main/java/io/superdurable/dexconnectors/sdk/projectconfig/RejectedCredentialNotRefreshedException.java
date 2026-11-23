// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * A forced refresh after a provider rejection returned no newer credential.
 */
public final class RejectedCredentialNotRefreshedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public RejectedCredentialNotRefreshedException(final String message) {
        super(message);
    }
}
