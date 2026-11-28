// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.*;

/** One page of visible worker summaries. */
public final class WorkerPage {
    /** Total number of matching workers. */
    public final long total;

    /** Workers on this page in provider order. */
    public final List<WorkerSummary> data;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public WorkerPage(
            @JsonProperty("total") long total, @JsonProperty("data") List<WorkerSummary> data) {
        this.total = total;
        this.data =
                data == null
                        ? Collections.emptyList()
                        : Collections.unmodifiableList(new ArrayList<>(data));
    }
}
