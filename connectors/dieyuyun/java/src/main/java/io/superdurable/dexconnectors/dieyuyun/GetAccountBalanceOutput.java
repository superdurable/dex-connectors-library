// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.dieyuyun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountBalanceOutput {
    /** Available yuan as the exact provider decimal. */
    public final String availableCNY;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public GetAccountBalanceOutput(@JsonProperty("availableCNY") String availableCNY) {
        this.availableCNY = availableCNY;
    }
}
