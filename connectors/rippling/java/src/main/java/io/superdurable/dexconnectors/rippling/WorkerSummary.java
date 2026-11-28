// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** The ID, job title and employment status of one visible worker. */
public final class WorkerSummary {
    /** Worker ID. */
    public final String id;

    /** Job title or an empty string if unavailable. */
    public final String title;

    /** Employment status or an empty string if unavailable. */
    public final String status;

    /** Restore the projected fields from Dex. */
    @JsonCreator
    public WorkerSummary(
            @JsonProperty("id") String id,
            @JsonProperty("title") String title,
            @JsonProperty("status") String status) {
        this.id = id == null ? "" : id;
        this.title = title == null ? "" : title;
        this.status = status == null ? "" : status;
    }
}
