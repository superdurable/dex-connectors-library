// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.io.IOException;

/**
 * Forces a coordinated refresh after one provider authentication rejection.
 *
 * @param <C> the connector-specific credential type
 */
public interface RejectedCredentialRefreshingProvider<C> extends CredentialProvider<C> {
    /**
     * Reloads and replaces rejected material under the connection lock.
     *
     * @param call the call identity
     * @param driver the connector-specific refresh decision and exchange
     * @return the replacement credentials
     * @throws IOException if storage or the exchange fails
     * @throws InterruptedException if the refresh is interrupted
     */
    C resolveAfterRejection(Call call, CredentialRefreshDriver<C> driver) throws IOException, InterruptedException;
}
