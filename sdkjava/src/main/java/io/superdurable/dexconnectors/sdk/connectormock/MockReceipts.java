// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.Receipt;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;

/** Builds the receipt every scripted answer returns. */
final class MockReceipts {
    private MockReceipts() {
    }

    static Receipt forCall(final Call call) {
        return Receipt.newBuilder()
                .callId(call.getId())
                .provider(call.getOperation().getConnectorId())
                .observedAt(Rfc3339NanoTimestamp.now())
                .idempotencyKey(call.getIdempotencyKey())
                .build();
    }
}
