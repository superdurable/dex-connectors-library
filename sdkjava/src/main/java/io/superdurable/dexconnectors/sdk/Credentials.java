// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.io.IOException;
import java.util.HashMap;
import java.util.Map;

/**
 * Resolves credentials through coordinated refresh when a provider supports it.
 */
public final class Credentials {
    private Credentials() {
    }

    /**
     * Resolves current credentials, refreshing them under the connection lock when supported.
     *
     * <p>A provider without refresh support performs an ordinary call-time lookup. Provider and
     * driver are required even when the provider is static.
     *
     * @param provider the credential provider
     * @param call the call identity
     * @param driver the connector-specific refresh driver
     * @param <C> the credential type
     * @return the credentials
     * @throws IOException if storage or the exchange fails
     * @throws InterruptedException if the lookup is interrupted
     */
    public static <C> C resolveCredential(
            final CredentialProvider<C> provider, final Call call, final CredentialRefreshDriver<C> driver)
            throws IOException, InterruptedException {
        if (provider == null || driver == null) {
            throw new IllegalArgumentException("credential provider and refresh driver are required");
        }
        if (provider instanceof RefreshingCredentialProvider) {
            return ((RefreshingCredentialProvider<C>) provider).resolveWithRefresh(call, driver);
        }
        return provider.resolve(call);
    }

    /**
     * Forces one coordinated refresh after a rejected access credential.
     *
     * <p>A connector calls this at most once per provider operation. Missing refresh support is an
     * error, so rejected credentials are never silently reused.
     *
     * @param provider the credential provider
     * @param call the call identity
     * @param driver the connector-specific refresh driver
     * @param <C> the credential type
     * @return the replacement credentials
     * @throws IOException if storage or the exchange fails
     * @throws InterruptedException if the refresh is interrupted
     * @throws IllegalArgumentException if the provider cannot refresh after rejection
     */
    public static <C> C resolveCredentialAfterRejection(
            final CredentialProvider<C> provider, final Call call, final CredentialRefreshDriver<C> driver)
            throws IOException, InterruptedException {
        if (provider == null || driver == null) {
            throw new IllegalArgumentException("credential provider and refresh driver are required");
        }
        if (!(provider instanceof RejectedCredentialRefreshingProvider)) {
            throw new IllegalArgumentException("credential provider does not support refresh after rejection");
        }
        return ((RejectedCredentialRefreshingProvider<C>) provider).resolveAfterRejection(call, driver);
    }

    /**
     * Creates a provider of in-memory credentials keyed by logical connection.
     *
     * @param credentials the credentials by connection
     * @param <C> the credential type
     * @return the static provider
     */
    public static <C> RefreshingCredentialProvider<C> staticProvider(final Map<ConnectionRef, C> credentials) {
        final Map<ConnectionRef, C> copy = new HashMap<>(credentials);
        return new RefreshingCredentialProvider<C>() {
            @Override
            public C resolve(final Call call) {
                final C value = copy.get(call.getConnection());
                if (value == null) {
                    throw new IllegalArgumentException("connection is not configured");
                }
                return value;
            }

            @Override
            public C resolveWithRefresh(final Call call, final CredentialRefreshDriver<C> driver) {
                // Static credentials have no expiry or persistence.
                return resolve(call);
            }
        };
    }
}
