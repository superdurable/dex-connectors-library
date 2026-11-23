// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * The credential broker no longer selects this shared connection for the scope.
 */
public final class ConnectionNotSelectedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     */
    public ConnectionNotSelectedException() {
        super("project connection is not selected by this scope");
    }
}
