// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.*;

/** One page of visible worker summaries. */
public final class WorkerPage {
    /** Workers visible to the token owner, in provider order. */
    public final List<WorkerSummary> workers;

    /** Opaque continuation cursor or an empty string on the last page. */
    public final String nextCursor;

    /** Restore one directory page from Dex. */
    @JsonCreator
    public WorkerPage(
            @JsonProperty("workers") List<WorkerSummary> workers,
            @JsonProperty("nextCursor") String nextCursor) {
        this.workers =
                workers == null
                        ? Collections.emptyList()
                        : Collections.unmodifiableList(new ArrayList<>(workers));
        this.nextCursor = nextCursor == null ? "" : nextCursor;
    }
}
