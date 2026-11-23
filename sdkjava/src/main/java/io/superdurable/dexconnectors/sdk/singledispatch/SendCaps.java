// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * Bounds of the single-dispatch send cap.
 */
public final class SendCaps {
    /** Smallest send cap; one send disables bounded resend. */
    public static final int MINIMUM_MAX_SENDS = 1;
    /** Largest send cap accepted by the Fence and the Step override. */
    public static final int MAXIMUM_MAX_SENDS = 10;

    private SendCaps() {
    }

    /**
     * Rejects a send cap outside 1..10.
     *
     * @param maxSends the send cap
     * @throws IllegalArgumentException if the cap is out of range
     */
    public static void validateMaxSends(final int maxSends) {
        if (maxSends < MINIMUM_MAX_SENDS || maxSends > MAXIMUM_MAX_SENDS) {
            throw new IllegalArgumentException("single-dispatch max sends must be an integer within 1..10");
        }
    }
}
