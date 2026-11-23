// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

/**
 * Selects which provider addresses a {@link ProviderHttpClient} may dial.
 */
public enum AddressPolicy {
    /** Only HTTPS to public unicast addresses. */
    PUBLIC_HTTPS("publicHTTPS"),
    /** HTTPS anywhere except link-local, plus plain HTTP to loopback or private addresses. */
    PRIVATE_NETWORK_HTTP("privateNetworkHTTP"),
    /** The caller-owned network behavior without address checks. */
    UNRESTRICTED("unrestricted");

    private final String manifestValue;

    AddressPolicy(final String manifestValue) {
        this.manifestValue = manifestValue;
    }

    /**
     * Returns the manifest and cross-language name of the policy.
     *
     * @return {@code publicHTTPS}, {@code privateNetworkHTTP}, or {@code unrestricted}
     */
    public String getManifestValue() {
        return manifestValue;
    }

    /**
     * Returns the policy with a manifest name.
     *
     * @param value the manifest name
     * @return the policy
     * @throws IllegalArgumentException if the name is unknown
     */
    public static AddressPolicy fromManifestValue(final String value) {
        for (AddressPolicy policy : values()) {
            if (policy.manifestValue.equals(value)) {
                return policy;
            }
        }
        throw new IllegalArgumentException("address policy is invalid");
    }
}
