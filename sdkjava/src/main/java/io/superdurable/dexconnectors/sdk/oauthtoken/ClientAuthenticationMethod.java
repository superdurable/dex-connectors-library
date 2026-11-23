// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

/**
 * OAuth token endpoint client authentication method.
 */
public enum ClientAuthenticationMethod {
    /** Client ID and secret in the request body. */
    CLIENT_SECRET_POST("client_secret_post"),
    /** Client ID and secret in an HTTP Basic {@code Authorization} header. */
    CLIENT_SECRET_BASIC("client_secret_basic"),
    /** A public client that sends only its client ID. */
    PUBLIC_CLIENT("none");

    private final String manifestValue;

    ClientAuthenticationMethod(final String manifestValue) {
        this.manifestValue = manifestValue;
    }

    /**
     * Returns the manifest {@code tokenEndpointAuthMethod} value.
     *
     * @return the manifest value
     */
    public String getManifestValue() {
        return manifestValue;
    }

    /**
     * Returns the method with a manifest value.
     *
     * @param value the manifest value
     * @return the method
     * @throws IllegalArgumentException if the value is unknown
     */
    public static ClientAuthenticationMethod fromManifestValue(final String value) {
        for (ClientAuthenticationMethod method : values()) {
            if (method.manifestValue.equals(value)) {
                return method;
            }
        }
        throw new IllegalArgumentException("OAuth client authentication method is not supported");
    }
}
