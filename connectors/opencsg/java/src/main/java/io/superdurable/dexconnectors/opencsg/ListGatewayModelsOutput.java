// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.opencsg;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Number of available models and the first fifty IDs in provider order. */
public final class ListGatewayModelsOutput {
    /** Total models in the provider response, including entries beyond the returned IDs. */
    public final int total;

    /** At most fifty IDs, preserving duplicates and ordering. */
    public final List<String> modelIDs;

    /** Decode one immutable catalog result from Dex wire fields. */
    @JsonCreator
    public ListGatewayModelsOutput(
            @JsonProperty("total") int total, @JsonProperty("modelIDs") List<String> modelIDs) {
        this.total = total;
        this.modelIDs = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(modelIDs));
    }
}
