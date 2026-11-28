// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sendcloud;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Balance snapshot returned by SendCloud. */
public final class GetAccountBalanceOutput {
    /** Available balance as the exact provider decimal. */
    public final String availableBalance;

    /** Total balance as the exact provider decimal. */
    public final String balance;

    /** Request allowance for the current day. */
    public final long dailyQuota;

    /** Requests used today. */
    public final long todayUsedQuota;

    /** Restore the balance and quota snapshot from Dex. */
    @JsonCreator
    public GetAccountBalanceOutput(
            @JsonProperty("availableBalance") String availableBalance,
            @JsonProperty("balance") String balance,
            @JsonProperty("dailyQuota") long dailyQuota,
            @JsonProperty("todayUsedQuota") long todayUsedQuota) {
        this.availableBalance = availableBalance;
        this.balance = balance;
        this.dailyQuota = dailyQuota;
        this.todayUsedQuota = todayUsedQuota;
    }
}
