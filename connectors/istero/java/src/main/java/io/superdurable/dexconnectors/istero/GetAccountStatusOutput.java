// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.istero;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountStatusOutput {
    /** Numeric ISAS account identifier. */
    public final long isid;

    /** Calls already used today. */
    public final long todayUsed;

    /** Calls remaining today. */
    public final long todayRemaining;

    /** Restore the provider snapshot from the Dex result. */
    @JsonCreator
    public GetAccountStatusOutput(
            @JsonProperty("isid") long isid,
            @JsonProperty("todayUsed") long todayUsed,
            @JsonProperty("todayRemaining") long todayRemaining) {
        this.isid = isid;
        this.todayUsed = todayUsed;
        this.todayRemaining = todayRemaining;
    }
}
