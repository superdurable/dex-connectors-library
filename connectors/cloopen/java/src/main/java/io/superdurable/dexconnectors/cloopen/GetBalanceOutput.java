// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.cloopen;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Balance snapshot returned by Cloopen. */
public final class GetBalanceOutput {
    /** Nonnegative decimal balance in provider units. */
    public final String balance;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public GetBalanceOutput(@JsonProperty("balance") String balance) {
        this.balance = balance;
    }
}
