// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;

/**
 * Decodes a verified webhook request into a Trigger event.
 *
 * @param <P> the payload type
 */
public interface WebhookEventDecoder<P> {
    /**
     * Decodes the request.
     *
     * @param request the verified request
     * @return the event, or {@code null} for a valid request no binding handles (200)
     * @throws Exception for an undecodable event; the endpoint answers 400
     */
    TriggerEvent<P> decodeEvent(WebhookRequest request) throws Exception;
}
