// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * A conditional create or compare-and-swap lost to another writer; re-read and decide again.
 */
public final class ObjectConflictException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public ObjectConflictException(final String message) {
        super(message);
    }
}
