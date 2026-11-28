// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.flurouter;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Predicate;
import java.util.HashSet;
import java.util.Set;

/** Ordered model IDs visible to the connected FluRouter API key. */
public final class ListModelsOutput {
    /** Unique printable model IDs, in the order returned by FluRouter. */
    public final List<String> modelIDs;

    /** Restore a catalog result from its Dex payload. */
    @JsonCreator public ListModelsOutput(@JsonProperty("modelIDs") List<String> modelIDs) {
        this.modelIDs = Collections.unmodifiableList(new ArrayList<>(modelIDs));
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode models, Predicate<String> containsCredential) {
        if (models.size() > 1000) return null;
        List<String> ids = new ArrayList<>(models.size());
        Set<String> seen = new HashSet<>();
        for (JsonNode model : models) {
            String id = model.path("id").textValue();
            if (!"model".equals(model.path("object").textValue()) || id == null || id.length() > 256 ||
                    !id.matches("[!-~]+") || !seen.add(id)) return null;
            ids.add(id);
        }
        return new ListModelsOutput(ids);
    }
}
