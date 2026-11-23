// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import java.nio.charset.StandardCharsets;
import java.util.AbstractMap;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;

/**
 * The endpoint's answer to one request.
 */
public final class WebhookResponse {
    private final int status;
    private final List<Map.Entry<String, String>> headers;
    private final byte[] body;

    /**
     * Creates a response.
     *
     * @param status the HTTP status code
     * @param headers the response header pairs
     * @param body the response body
     */
    public WebhookResponse(final int status, final List<Map.Entry<String, String>> headers, final byte[] body) {
        this.status = status;
        this.headers = headers == null ? Collections.<Map.Entry<String, String>>emptyList()
                : Collections.unmodifiableList(new ArrayList<>(headers));
        this.body = body == null ? new byte[0] : body.clone();
    }

    static WebhookResponse empty(final int status) {
        return new WebhookResponse(status, null, null);
    }

    static WebhookResponse text(final int status, final String text, final String... extraHeaderPairs) {
        final List<Map.Entry<String, String>> headers = new ArrayList<>();
        headers.add(new AbstractMap.SimpleImmutableEntry<>("content-type", "text/plain; charset=utf-8"));
        for (int index = 0; index + 1 < extraHeaderPairs.length; index += 2) {
            headers.add(new AbstractMap.SimpleImmutableEntry<>(extraHeaderPairs[index], extraHeaderPairs[index + 1]));
        }
        return new WebhookResponse(status, headers, (text + "\n").getBytes(StandardCharsets.UTF_8));
    }

    /**
     * Returns the HTTP status code.
     *
     * @return the status
     */
    public int getStatus() {
        return status;
    }

    /**
     * Returns the response header pairs.
     *
     * @return the unmodifiable headers
     */
    public List<Map.Entry<String, String>> getHeaders() {
        return headers;
    }

    /**
     * Returns a copy of the response body.
     *
     * @return the body bytes
     */
    public byte[] getBody() {
        return body.clone();
    }

    /**
     * Returns the body as UTF-8 text.
     *
     * @return the body text
     */
    public String getBodyText() {
        return new String(body, StandardCharsets.UTF_8);
    }
}
