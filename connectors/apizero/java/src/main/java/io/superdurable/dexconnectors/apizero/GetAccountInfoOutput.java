// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apizero;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
@com.fasterxml.jackson.annotation.JsonInclude(
        com.fasterxml.jackson.annotation.JsonInclude.Include.NON_NULL)
public final class GetAccountInfoOutput {
    /** Scope of the connected key. */
    public final String scope;

    /** Wallet balance in yuan for account scope. */
    public final String balanceYuan;

    /** Remaining prepaid calls for key scope. */
    public final Long keyQuotaRemaining;

    /** Restore the provider snapshot from the Dex result. */
    @JsonCreator
    public GetAccountInfoOutput(
            @JsonProperty("scope") String scope,
            @JsonProperty("balanceYuan") String balanceYuan,
            @JsonProperty("keyQuotaRemaining") Long keyQuotaRemaining) {
        this.scope = scope;
        this.balanceYuan = balanceYuan;
        this.keyQuotaRemaining = keyQuotaRemaining;
    }
}
