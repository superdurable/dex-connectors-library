// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** System key and displayed ID of one invoice. */
public final class InvoiceSummary {
    /** Invoice system key. */
    public final String key;

    /** Displayed invoice ID. */
    public final String id;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public InvoiceSummary(@JsonProperty("key") String key, @JsonProperty("id") String id) {
        this.key = key == null ? "" : key;
        this.id = id == null ? "" : id;
    }
}
