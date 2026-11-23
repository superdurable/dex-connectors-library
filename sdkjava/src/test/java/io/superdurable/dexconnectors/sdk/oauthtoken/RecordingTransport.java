// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpHeaders;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpRequest;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpResponse;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;

/** Records token requests and answers with a scripted JSON body. */
final class RecordingTransport implements ProviderHttpTransport {
    final List<ProviderHttpRequest> requests = new CopyOnWriteArrayList<>();
    volatile int statusCode = 200;
    volatile byte[] body = new byte[0];
    volatile Duration bodyDelay = Duration.ZERO;

    @Override
    public ProviderHttpResponse send(final ProviderHttpRequest request) throws IOException {
        requests.add(request);
        final byte[] contents = body;
        final Duration delay = bodyDelay;
        final InputStream stream = new ByteArrayInputStream(contents) {
            private boolean hasWaited;

            @Override
            public synchronized int read(final byte[] buffer, final int offset, final int length) {
                if (!hasWaited && !delay.isZero()) {
                    hasWaited = true;
                    try {
                        Thread.sleep(delay.toMillis());
                    } catch (InterruptedException error) {
                        Thread.currentThread().interrupt();
                        return -1;
                    }
                }
                return super.read(buffer, offset, length);
            }
        };
        return new ProviderHttpResponse(statusCode, new ProviderHttpHeaders(
                Collections.singletonMap("Content-Type", Collections.singletonList("application/json"))), stream, null);
    }
}
