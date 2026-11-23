// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

/**
 * Chooses whether a stored credential without an expiry is refreshable or permanent.
 */
public enum MissingExpiryRule {
    /** Refresh a credential whose expiry is unknown. */
    REFRESH_WHEN_EXPIRY_MISSING,
    /** Keep a credential whose expiry is unknown. */
    KEEP_WHEN_EXPIRY_MISSING
}
