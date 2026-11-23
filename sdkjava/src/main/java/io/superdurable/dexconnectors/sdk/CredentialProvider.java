// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.io.IOException;

/**
 * Resolves current credentials at call time, outside durable Flow state.
 *
 * <p>Implementations may also implement {@link RefreshingCredentialProvider} and
 * {@link RejectedCredentialRefreshingProvider}; {@link Credentials} dispatches to them.
 *
 * @param <C> the connector-specific credential type
 */
public interface CredentialProvider<C> {
    /**
     * Returns connector-specific credentials for one provider call.
     *
     * @param call the call identity
     * @return the credentials
     * @throws IOException if credential storage cannot be read
     * @throws InterruptedException if the lookup is interrupted
     */
    C resolve(Call call) throws IOException, InterruptedException;
}
