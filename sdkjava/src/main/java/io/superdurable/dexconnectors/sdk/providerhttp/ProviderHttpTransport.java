// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.io.IOException;

/**
 * Sends one provider HTTP request without following redirects.
 *
 * <p>{@link ProviderHttpClient} is the production implementation; tests may supply a fake.
 */
public interface ProviderHttpTransport {
    /**
     * Sends a request once and returns the streaming response.
     *
     * @param request the request
     * @return the response, owned by the caller
     * @throws AddressRefusalException if an address policy refused the request; nothing was sent
     * @throws ProviderTransportException if the transport failed
     * @throws IOException if the request failed in another way
     */
    ProviderHttpResponse send(ProviderHttpRequest request) throws IOException;
}
