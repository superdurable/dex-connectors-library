// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Select one worker by their Workday WID. */
public final class GetWorkerInput {
    /** Workday 32-character worker WID. */
    public final String workerId;

    /** Construct the GetWorkerInput wire value. Omitted text fields default to empty. */
    @JsonCreator
    public GetWorkerInput(@JsonProperty("workerId") String workerId) {
        this.workerId = workerId == null ? "" : workerId;
    }
}
