// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * Credential refresh failed without proving the prior credential invalid; retry later.
 *
 * <p>The provider's error text is deliberately not kept, because it may contain credentials.
 */
public final class RefreshFailedException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param message the secret-safe message
     */
    public RefreshFailedException(final String message) {
        super(message);
    }
}
