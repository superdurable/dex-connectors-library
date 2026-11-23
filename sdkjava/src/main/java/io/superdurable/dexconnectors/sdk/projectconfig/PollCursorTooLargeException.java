// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * A poll cursor state exceeds a storage bound; no retry of the same state can succeed.
 */
public final class PollCursorTooLargeException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public PollCursorTooLargeException(final String message) {
        super(message);
    }
}
