// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.databind.annotation.JsonSerialize;
import io.superdurable.dexconnectors.sdk.PrivateValueRefusingSerializer;
import io.superdurable.dexconnectors.sdk.SecretString;

/**
 * OAuth client identity and redacted secret used for one token grant.
 */
@JsonSerialize(using = PrivateValueRefusingSerializer.class)
public final class ClientCredentials {
    private final String id;
    private final SecretString secret;
    private final ClientAuthenticationMethod authenticationMethod;

    /**
     * Creates client credentials.
     *
     * @param id the client ID
     * @param secret the client secret; empty for a public client
     * @param authenticationMethod the token endpoint authentication method
     */
    public ClientCredentials(
            final String id, final SecretString secret, final ClientAuthenticationMethod authenticationMethod) {
        if (id == null || secret == null || authenticationMethod == null) {
            throw new IllegalArgumentException("client ID, secret, and authentication method are required");
        }
        this.id = id;
        this.secret = secret;
        this.authenticationMethod = authenticationMethod;
    }

    /**
     * Returns the client ID.
     *
     * @return the client ID
     */
    public String getId() {
        return id;
    }

    /**
     * Returns the redacted client secret.
     *
     * @return the secret
     */
    public SecretString getSecret() {
        return secret;
    }

    /**
     * Returns the token endpoint authentication method.
     *
     * @return the method
     */
    public ClientAuthenticationMethod getAuthenticationMethod() {
        return authenticationMethod;
    }

    /**
     * Returns the authentication method only, omitting the client ID and secret.
     *
     * @return the redacted description
     */
    @Override
    public String toString() {
        return "ClientCredentials{" + authenticationMethod + "}";
    }
}
