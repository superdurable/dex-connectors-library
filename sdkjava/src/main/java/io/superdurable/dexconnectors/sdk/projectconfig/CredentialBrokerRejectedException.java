// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * Terminal credential broker refusal or invalid wire answer; retrying the same read cannot help.
 */
public class CredentialBrokerRejectedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public CredentialBrokerRejectedException(final String message) {
        super(message);
    }
}
