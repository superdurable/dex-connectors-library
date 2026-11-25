// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * A stored poll cursor document fails strict decoding; the store never overwrites it.
 */
public final class PollCursorDocumentInvalidException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public PollCursorDocumentInvalidException(final String message) {
        super(message);
    }
}
