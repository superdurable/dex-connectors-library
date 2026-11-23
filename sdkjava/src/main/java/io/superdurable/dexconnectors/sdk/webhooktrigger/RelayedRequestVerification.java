// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.projectconfig.CredentialBrokerUnavailableException;

/**
 * Builds webhook verifiers for requests a Dex host relays.
 */
public final class RelayedRequestVerification {
    private RelayedRequestVerification() {
    }

    /**
     * Returns a request verifier that checks the Dex relay signature with the given verifier.
     *
     * <p>A {@link CredentialBrokerUnavailableException} becomes
     * {@link WebhookVerificationUnavailableException} (503); every other error is a refusal (400).
     *
     * @param verifier the relayed request verifier, such as a loaded project
     * @param <C> the endpoint credential type, which relay verification ignores
     * @return the request verifier
     * @throws IllegalArgumentException without a verifier
     */
    public static <C> WebhookRequestVerifier<C> newRelayedRequestVerification(final RelayedRequestVerifier verifier) {
        if (verifier == null) {
            throw new IllegalArgumentException("relayed request verifier is required");
        }
        return (request, credentials, cancellation) -> {
            try {
                verifier.verifyRelayedRequest(request.getHeaders(), request.getBody(), request.getReceivedAt());
            } catch (CredentialBrokerUnavailableException error) {
                throw new WebhookVerificationUnavailableException(error);
            }
        };
    }
}
