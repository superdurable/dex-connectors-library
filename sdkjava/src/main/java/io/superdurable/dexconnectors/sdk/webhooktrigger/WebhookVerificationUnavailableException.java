// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

/**
 * Verification cannot run now, such as an unreadable secret; the endpoint answers 503 for a retry.
 */
public final class WebhookVerificationUnavailableException extends Exception {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception, optionally caused by another failure.
     *
     * @param cause the underlying failure, or {@code null}
     */
    public WebhookVerificationUnavailableException(final Throwable cause) {
        super("webhook verification is temporarily unavailable", cause);
    }
}
