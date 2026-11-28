// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tripo;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountBalanceOutput {
    /** Available credits as the exact provider decimal. */
    public final String availableCredits;

    /** Reserved credits as the exact provider decimal. */
    public final String frozenCredits;

    /** Restore the snapshot from the Dex result. */
    @JsonCreator
    public GetAccountBalanceOutput(
            @JsonProperty("availableCredits") String availableCredits,
            @JsonProperty("frozenCredits") String frozenCredits) {
        this.frozenCredits = frozenCredits;
        this.availableCredits = availableCredits;
    }
}
