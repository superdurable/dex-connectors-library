// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.chuanglan;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Balance snapshot returned by Chuanglan. */
public final class GetBalanceOutput {
    /** Remaining domestic SMS message count. */
    public final long remainingMessages;

    /** Restore the balance snapshot from Dex. */
    @JsonCreator
    public GetBalanceOutput(@JsonProperty("remainingMessages") long remainingMessages) {
        this.remainingMessages = remainingMessages;
    }
}
