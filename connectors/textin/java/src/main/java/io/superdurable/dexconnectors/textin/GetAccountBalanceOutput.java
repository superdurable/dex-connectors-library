// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.textin;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountBalanceOutput {
    /** Available T-coins as the exact provider decimal. */
    public final String availableTCoins;

    /** One for prepaid and two for postpaid. */
    public final int payType;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public GetAccountBalanceOutput(
            @JsonProperty("availableTCoins") String availableTCoins,
            @JsonProperty("payType") int payType) {
        this.availableTCoins = availableTCoins;
        this.payType = payType;
    }
}
