// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Terminal credential state that requires an interactive authorization flow.
 *
 * <p>The message never contains provider details or credentials.
 */
public final class ReauthorizationRequiredException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception without a cause.
     */
    public ReauthorizationRequiredException() {
        super("connector reauthorization is required");
    }

    /**
     * Creates the exception with a secret-safe cause.
     *
     * @param cause the underlying failure
     */
    public ReauthorizationRequiredException(final Throwable cause) {
        super("connector reauthorization is required", cause);
    }
}
