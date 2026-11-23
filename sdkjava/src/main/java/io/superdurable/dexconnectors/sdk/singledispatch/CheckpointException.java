// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * The claim heartbeat failed before the provider request was sent.
 */
public final class CheckpointException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    CheckpointException(final Throwable cause) {
        super("single-dispatch checkpoint could not be recorded", cause);
    }
}
