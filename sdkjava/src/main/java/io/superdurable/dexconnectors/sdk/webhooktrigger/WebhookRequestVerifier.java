// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.trigger.TriggerCancellation;

/**
 * Verifies one webhook request with the connection's credentials.
 *
 * @param <C> the credential type
 */
public interface WebhookRequestVerifier<C> {
    /**
     * Verifies a request.
     *
     * @param request the request with its complete body
     * @param credentials the connection's resolved credentials
     * @param cancellation fires when the request is abandoned
     * @throws WebhookVerificationUnavailableException for a temporary failure; the endpoint answers 503
     * @throws Exception for a refusal; the endpoint answers 400
     */
    void verifyRequest(WebhookRequest request, C credentials, TriggerCancellation cancellation) throws Exception;
}
