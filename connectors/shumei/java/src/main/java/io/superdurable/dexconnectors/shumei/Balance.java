// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.shumei;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Balance snapshot returned by Shumei. */
public final class Balance {
    /** Available balance in Chinese yuan. */
    public final double balanceYuan;

    /** Restore the balance snapshot from Dex. */
    @JsonCreator
    public Balance(@JsonProperty("balanceYuan") double balanceYuan) {
        this.balanceYuan = balanceYuan;
    }
}
