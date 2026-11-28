// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ppio;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Predicate;

/** PPIO models in the order returned by the authorized catalog. */
public final class ListModelsOutput {
    /** Projected models with their owner and creation timestamp. */
    public final List<Model> models;

    /** Restore the complete ordered catalog from Dex. */
    @JsonCreator public ListModelsOutput(@JsonProperty("models") List<Model> models) {
        this.models = Collections.unmodifiableList(new ArrayList<>(models));
    }

    /** Immutable provider metadata for a single model. */
    public static final class Model {
        /** Provider model identifier. */
        public final String id;
        /** Creation time in Unix seconds. */
        public final long createdAtUnix;
        /** Provider organization. */
        public final String ownedBy;
        /** Human-readable model name. */
        public final String displayName;
        /** Provider availability status. */
        public final long status;
        /** Maximum context size in tokens. */
        public final long contextSize;
        /** Supported API endpoint families. */
        public final List<String> endpoints;

        /** Restore one model from its Dex wire representation. */
        @JsonCreator public Model(@JsonProperty("id") String id,
                @JsonProperty("createdAtUnix") long createdAtUnix,
                @JsonProperty("ownedBy") String ownedBy,
                @JsonProperty("displayName") String displayName,
                @JsonProperty("status") long status,
                @JsonProperty("contextSize") long contextSize,
                @JsonProperty("endpoints") List<String> endpoints) {
            this.id = id;
            this.createdAtUnix = createdAtUnix;
            this.ownedBy = ownedBy;
            this.displayName = displayName;
            this.status = status;
            this.contextSize = contextSize;
            this.endpoints = Collections.unmodifiableList(new ArrayList<>(endpoints));
        }
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode rows, Predicate<String> containsCredential) {
        List<Model> models = new ArrayList<>(rows.size());
        for (JsonNode row : rows) {
            String id = row.path("id").textValue();
            String owner = row.path("owned_by").textValue();
            String displayName = row.path("display_name").textValue();
            JsonNode created = row.path("created");
            JsonNode status = row.path("status");
            JsonNode contextSize = row.path("context_size");
            JsonNode endpoints = row.path("endpoints");
            if (!"model".equals(row.path("object").textValue()) || id == null || id.isEmpty() ||
                    !"ppio".equals(owner) || displayName == null || displayName.isEmpty() ||
                    !isLong(created) || created.longValue() < 0 || !isLong(status) ||
                    (status.longValue() != 1 && status.longValue() != 3 && status.longValue() != 4) ||
                    !isLong(contextSize) || contextSize.longValue() <= 0 || !endpoints.isArray()) return null;
            List<String> families = new ArrayList<>(endpoints.size());
            for (JsonNode endpoint : endpoints) {
                if (!endpoint.isNull() && !endpoint.isTextual()) return null;
                families.add(endpoint.asText(""));
            }
            models.add(new Model(id, created.longValue(), owner, displayName, status.longValue(),
                    contextSize.longValue(), families));
        }
        return new ListModelsOutput(models);
    }

    private static boolean isLong(JsonNode value) {
        return value.isIntegralNumber() && value.canConvertToLong();
    }
}
