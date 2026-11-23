// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dexconnectors.sdk.SecretString;
import java.time.Duration;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Complete bounded OAuth token response with redacted token material.
 *
 * <p>{@link #toString()} omits every token and retained value.
 */
public final class TokenResponse {
    private final SecretString accessToken;
    private final SecretString refreshToken;
    private final Duration expiresIn;
    private final Duration refreshTokenExpiresIn;
    private final String scope;
    private final String tokenType;
    private final Map<String, JsonNode> retainedFields;
    private final Map<String, SecretString> responseStrings;

    /**
     * Creates a token response.
     *
     * @param accessToken the access token
     * @param refreshToken the rotated refresh token, or empty
     * @param expiresIn the access token lifetime, or zero when undeclared
     * @param refreshTokenExpiresIn the refresh token lifetime, or zero
     * @param scope the granted scope value, or empty
     * @param tokenType the token type, or empty
     * @param retainedFields connector-declared nonstandard response fields
     * @param responseStrings every string in the response object by dotted path
     */
    public TokenResponse(
            final SecretString accessToken, final SecretString refreshToken, final Duration expiresIn,
            final Duration refreshTokenExpiresIn, final String scope, final String tokenType,
            final Map<String, JsonNode> retainedFields, final Map<String, SecretString> responseStrings) {
        this.accessToken = accessToken;
        this.refreshToken = refreshToken;
        this.expiresIn = expiresIn;
        this.refreshTokenExpiresIn = refreshTokenExpiresIn;
        this.scope = scope;
        this.tokenType = tokenType;
        this.retainedFields = Collections.unmodifiableMap(new LinkedHashMap<>(retainedFields));
        this.responseStrings = Collections.unmodifiableMap(new LinkedHashMap<>(responseStrings));
    }

    /**
     * Returns the access token.
     *
     * @return the access token
     */
    public SecretString getAccessToken() {
        return accessToken;
    }

    /**
     * Returns the rotated refresh token.
     *
     * @return the refresh token, empty when the provider did not rotate it
     */
    public SecretString getRefreshToken() {
        return refreshToken;
    }

    /**
     * Returns the access token lifetime.
     *
     * @return the lifetime, zero when undeclared
     */
    public Duration getExpiresIn() {
        return expiresIn;
    }

    /**
     * Returns the refresh token lifetime.
     *
     * @return the lifetime, zero when undeclared
     */
    public Duration getRefreshTokenExpiresIn() {
        return refreshTokenExpiresIn;
    }

    /**
     * Returns the granted scope value.
     *
     * @return the scope, or empty
     */
    public String getScope() {
        return scope;
    }

    /**
     * Returns the token type.
     *
     * @return the token type, or empty
     */
    public String getTokenType() {
        return tokenType;
    }

    /**
     * Returns connector-declared nonstandard response fields that were present.
     *
     * @return the unmodifiable retained fields
     */
    public Map<String, JsonNode> getRetainedFields() {
        return retainedFields;
    }

    /**
     * Uses rotated refresh material when present, otherwise the prior token.
     *
     * @param prior the stored refresh token
     * @return the refresh token to store
     */
    public SecretString nextRefreshToken(final SecretString prior) {
        return refreshToken.isEmpty() ? prior : refreshToken;
    }

    /**
     * Reads a manifest-mapped string, such as {@code authed_user.access_token}.
     *
     * @param path the dotted path through response objects
     * @return the string, or {@code null} when absent or not a string
     */
    public String mappedResponseString(final String path) {
        final SecretString value = responseStrings.get(path);
        return value == null ? null : value.reveal();
    }

    /**
     * Returns the token type and lifetime, omitting every token.
     *
     * @return the redacted description
     */
    @Override
    public String toString() {
        return "TokenResponse{tokenType=" + tokenType + ", expiresIn=" + expiresIn + "}";
    }
}
