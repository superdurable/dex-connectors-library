// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * The write window cannot fit the minimum request time, so nothing was sent.
 *
 * <p>Return a Retry. When the {@code notSent} heartbeat also failed, it is the cause.
 */
public final class InsufficientAttemptTimeException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    InsufficientAttemptTimeException(final String message, final Throwable cause) {
        super(message, cause);
    }
}
