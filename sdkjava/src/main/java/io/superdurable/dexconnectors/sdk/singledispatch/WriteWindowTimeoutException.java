// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import java.io.InterruptedIOException;

/**
 * The provider send passed the Fence write deadline and was cancelled.
 *
 * <p>The request may have reached the provider. {@link #getObservedStatus()} is the HTTP status
 * of the request that was cut off, or {@code null} when its headers had not arrived; each new
 * request clears the status of an earlier one. Requests sent through the SDK provider HTTP client
 * record it. A 2xx status means the provider accepted the request although its body was not read
 * in time.
 */
public final class WriteWindowTimeoutException extends InterruptedIOException {
    private static final long serialVersionUID = 1L;

    private final Integer observedStatus;

    WriteWindowTimeoutException(final Integer observedStatus) {
        super("provider send exceeded the single-dispatch write window");
        this.observedStatus = observedStatus;
    }

    /**
     * Returns the HTTP status of the cut-off request.
     *
     * @return the status, or {@code null} before its headers arrived
     */
    public Integer getObservedStatus() {
        return observedStatus;
    }
}
