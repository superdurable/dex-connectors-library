// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.stepfun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Predicate;

/** StepFun models in the order returned by the authorized catalog. */
public final class ListModelsOutput {
    /** Projected models with their owner and creation timestamp. */
    public final List<Model> models;

    /** Restore the complete ordered catalog from Dex. */
    @JsonCreator public ListModelsOutput(@JsonProperty("models") List<Model> models) {
        this.models = Collections.unmodifiableList(new ArrayList<>(models));
    }

    /** Immutable provider metadata for a single model. */
    public static final class Model {
        /** Model identifier accepted by the inference API. */
        public final String id;
        /** Model creation time, expressed in Unix seconds. */
        public final long createdAtUnix;
        /** Name of the model's owning organization. */
        public final String ownedBy;

        /** Restore one model from its Dex wire representation. */
        @JsonCreator public Model(@JsonProperty("id") String id, @JsonProperty("createdAtUnix") long createdAtUnix,
                                  @JsonProperty("ownedBy") String ownedBy) {
            this.id = id;
            this.createdAtUnix = createdAtUnix;
            this.ownedBy = ownedBy;
        }
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode rows, Predicate<String> containsCredential) {
        List<Model> models = new ArrayList<>(rows.size());
        for (JsonNode row : rows) {
            String id = row.path("id").textValue();
            String owner = row.path("owned_by").textValue();
            JsonNode created = row.path("created");
            if (!"model".equals(row.path("object").textValue()) || id == null || id.isEmpty() ||
                    owner == null || owner.isEmpty() || !created.isIntegralNumber() ||
                    !created.canConvertToLong() || created.longValue() < 0) return null;
            models.add(new Model(id, created.longValue(), owner));
        }
        return new ListModelsOutput(models);
    }
}
