// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import java.time.Duration;
import java.time.Instant;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Deterministic OAuth refresh decisions shared by every connector.
 */
public final class OAuthRefreshRules {
    /** Largest margin before expiry at which a token is refreshed. */
    public static final Duration REFRESH_SKEW = Duration.ofMinutes(5);

    private static final Duration FORCED_REFRESH_FLOOR = Duration.ofMinutes(1);

    private OAuthRefreshRules() {
    }

    /**
     * Returns the smaller of five minutes and one quarter of the token lifetime.
     *
     * @param issuedAt the issue time, or {@code null}
     * @param expiresAt the expiry, or {@code null}
     * @return the refresh margin; five minutes when the lifetime is unknown
     */
    public static Duration proportionalRefreshSkew(final Instant issuedAt, final Instant expiresAt) {
        return quarterLifetimeOrLimit(issuedAt, expiresAt, REFRESH_SKEW);
    }

    /**
     * Bounds repeated forced refreshes to one minute or a quarter of the token lifetime.
     *
     * @param issuedAt the issue time, or {@code null}
     * @param expiresAt the expiry, or {@code null}
     * @return the minimum interval between forced refreshes
     */
    public static Duration forcedRefreshFloor(final Instant issuedAt, final Instant expiresAt) {
        return quarterLifetimeOrLimit(issuedAt, expiresAt, FORCED_REFRESH_FLOOR);
    }

    /**
     * Decides refresh from token presence, optional expiry, and expiry skew.
     *
     * @param hasAccessToken whether a usable access token is stored
     * @param expiresAt the expiry, or {@code null} when unknown
     * @param now the decision time
     * @param rule the behavior for an unknown expiry
     * @param issuedAt the issue time, or {@code null}
     * @return {@code true} when the credential must be refreshed
     */
    public static boolean isRefreshRequired(
            final boolean hasAccessToken, final Instant expiresAt, final Instant now,
            final MissingExpiryRule rule, final Instant issuedAt) {
        if (!hasAccessToken) {
            return true;
        }
        if (expiresAt == null) {
            return rule != MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING;
        }
        return !now.isBefore(expiresAt.minus(proportionalRefreshSkew(issuedAt, expiresAt)));
    }

    /**
     * Accepts every required scope in a space- or comma-separated OAuth value.
     *
     * @param returnedScope the provider's scope value
     * @param requiredScopes the required scopes
     * @return {@code true} when every required scope was granted
     */
    public static boolean hasAllScopes(final String returnedScope, final List<String> requiredScopes) {
        return splitScopes(returnedScope).containsAll(requiredScopes);
    }

    /**
     * Requires equal scope sets, accepting spaces or commas as separators.
     *
     * @param returnedScope the provider's scope value
     * @param requiredScopes the required scopes
     * @return {@code true} when the granted set equals the required set
     */
    public static boolean hasExactScopes(final String returnedScope, final List<String> requiredScopes) {
        return splitScopes(returnedScope).equals(new HashSet<>(requiredScopes));
    }

    private static Set<String> splitScopes(final String value) {
        final Set<String> scopes = new HashSet<>(Arrays.asList(value.split("[ ,]+")));
        scopes.remove("");
        return scopes;
    }

    private static Duration quarterLifetimeOrLimit(final Instant issuedAt, final Instant expiresAt, final Duration limit) {
        if (issuedAt == null || expiresAt == null || !expiresAt.isAfter(issuedAt)) {
            return limit;
        }
        final Duration quarter = Duration.between(issuedAt, expiresAt).dividedBy(4);
        return quarter.compareTo(limit) < 0 ? quarter : limit;
    }
}
