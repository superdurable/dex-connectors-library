// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import java.time.Instant;
import java.util.List;
import java.util.Map;

/**
 * Verifies that a Dex host signed a relayed request, as a loaded project does.
 */
public interface RelayedRequestVerifier {
    /**
     * Throws when the request is not validly signed.
     *
     * @param headers the request header pairs
     * @param body the raw body
     * @param receivedAt when the request was received
     * @throws Exception when the request is refused; a
     *     {@code CredentialBrokerUnavailableException} when verification is temporarily unavailable
     */
    void verifyRelayedRequest(List<Map.Entry<String, String>> headers, byte[] body, Instant receivedAt)
            throws Exception;
}
