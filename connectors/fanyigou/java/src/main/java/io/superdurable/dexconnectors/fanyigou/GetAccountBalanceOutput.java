// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.fanyigou;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Balance snapshot returned by Fanyigou. */
public final class GetAccountBalanceOutput {
    /** Remaining document resource quantity. */
    public final long balance;

    /** Zero for points and one for pages. */
    public final int deductType;

    /** Restore the balance snapshot from Dex. */
    @JsonCreator
    public GetAccountBalanceOutput(
            @JsonProperty("deductType") int deductType, @JsonProperty("balance") long balance) {
        this.balance = balance;
        this.deductType = deductType;
    }
}
