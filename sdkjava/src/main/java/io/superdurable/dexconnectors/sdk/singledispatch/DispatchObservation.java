// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * Records the HTTP status of the latest request of the current Fence dispatch.
 *
 * <p>Provider HTTP helpers call these methods from the thread that runs a Fence send; outside a
 * dispatch they are no-ops. A {@link WriteWindowTimeoutException} carries the recorded status.
 */
public final class DispatchObservation {
    private static final ThreadLocal<DispatchObservation> CURRENT = new ThreadLocal<>();

    private volatile Integer statusCode;

    DispatchObservation() {
    }

    /**
     * Records a response status for the enclosing Fence dispatch.
     *
     * @param statusCode the HTTP status code
     */
    public static void recordResponseStatus(final int statusCode) {
        final DispatchObservation observation = CURRENT.get();
        if (observation != null) {
            observation.statusCode = statusCode;
        }
    }

    /**
     * Forgets an earlier request's status as a new request starts.
     */
    public static void clearResponseStatus() {
        final DispatchObservation observation = CURRENT.get();
        if (observation != null) {
            observation.statusCode = null;
        }
    }

    static void bind(final DispatchObservation observation) {
        CURRENT.set(observation);
    }

    static void unbind() {
        CURRENT.remove();
    }

    Integer statusCode() {
        return statusCode;
    }
}
