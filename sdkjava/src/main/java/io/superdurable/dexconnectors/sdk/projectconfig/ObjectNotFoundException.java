// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * The requested project object or exact version does not exist.
 */
public final class ObjectNotFoundException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public ObjectNotFoundException(final String message) {
        super(message);
    }
}
