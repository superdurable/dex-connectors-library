// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tokenfactory;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class Balance {
    /** Available balance in Chinese yuan. */
    public final double balanceYuan;

    /** Whether the account is active. */
    public final boolean isActive;

    /** Currency code returned by the provider. */
    public final String unit;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public Balance(
            @JsonProperty("balanceYuan") double balanceYuan,
            @JsonProperty("isActive") boolean isActive,
            @JsonProperty("unit") String unit) {
        this.balanceYuan = balanceYuan;
        this.isActive = isActive;
        this.unit = unit;
    }
}
