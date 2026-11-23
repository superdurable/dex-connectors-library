// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.io.IOException;

/**
 * Resolves, refreshes, and persists credentials under a connection lock.
 *
 * <p>Implementations reload current material after acquiring the lock, then atomically persist
 * complete replacement material before returning it.
 *
 * @param <C> the connector-specific credential type
 */
public interface RefreshingCredentialProvider<C> extends CredentialProvider<C> {
    /**
     * Returns a usable credential, refreshing it when the driver requires.
     *
     * @param call the call identity
     * @param driver the connector-specific refresh decision and exchange
     * @return the credentials
     * @throws IOException if storage or the exchange fails
     * @throws InterruptedException if the refresh is interrupted
     */
    C resolveWithRefresh(Call call, CredentialRefreshDriver<C> driver) throws IOException, InterruptedException;
}
