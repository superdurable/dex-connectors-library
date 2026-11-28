// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yunpian;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountBalanceOutput {
    /** Remaining balance in account currency units. */
    public final double balance;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public GetAccountBalanceOutput(@JsonProperty("balance") double balance) {
        this.balance = balance;
    }
}
