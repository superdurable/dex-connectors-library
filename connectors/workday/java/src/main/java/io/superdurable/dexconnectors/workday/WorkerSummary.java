// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** The WID and display name of one visible worker. */
public final class WorkerSummary {
    /** Worker WID. */
    public final String id;

    /** Display name supplied by the tenant. */
    public final String descriptor;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public WorkerSummary(
            @JsonProperty("id") String id, @JsonProperty("descriptor") String descriptor) {
        this.id = id == null ? "" : id;
        this.descriptor = descriptor == null ? "" : descriptor;
    }
}
