// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.infiniai;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Predicate;

/** InfiniAI models in the order returned by the authorized catalog. */
public final class ListModelsOutput {
    /** Projected models with their owner and creation timestamp. */
    public final List<ModelInfo> models;

    /** Restore the complete ordered catalog from Dex. */
    @JsonCreator public ListModelsOutput(@JsonProperty("models") List<ModelInfo> models) {
        this.models = Collections.unmodifiableList(new ArrayList<>(models));
    }

    /** Immutable provider metadata for a single model. */
    public static final class ModelInfo {
        /** GenStudio model identifier. */
        public final String id;
        /** Provider model category. */
        public final String modelType;
        /** Maximum context in tokens. */
        public final long contextLength;
        /** Maximum output in tokens. */
        public final long maxOutputLength;

        /** Restore one model from its Dex wire representation. */
        @JsonCreator public ModelInfo(@JsonProperty("id") String id,
                @JsonProperty("modelType") String modelType,
                @JsonProperty("contextLength") long contextLength,
                @JsonProperty("maxOutputLength") long maxOutputLength) {
            this.id = id;
            this.modelType = modelType;
            this.contextLength = contextLength;
            this.maxOutputLength = maxOutputLength;
        }
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode rows, Predicate<String> containsCredential) {
        List<ModelInfo> models = new ArrayList<>(rows.size());
        for (JsonNode row : rows) {
            String id = row.path("id").textValue();
            String modelType = row.path("model_type").textValue();
            JsonNode context = row.path("context_length");
            JsonNode output = row.path("max_output_length");
            if (!"model".equals(row.path("object").textValue()) || id == null || id.isEmpty() ||
                    id.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 512 || containsCredential.test(id) ||
                    modelType == null || modelType.isEmpty() || containsCredential.test(modelType) ||
                    modelType.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 128 ||
                    !isLong(row.path("created")) || !row.path("owned_by").isTextual() ||
                    !isLong(context) || context.longValue() < 0 || !isLong(output) || output.longValue() < 0) return null;
            models.add(new ModelInfo(id, modelType, context.longValue(), output.longValue()));
        }
        return new ListModelsOutput(models);
    }

    private static boolean isLong(JsonNode value) {
        return value.isIntegralNumber() && value.canConvertToLong();
    }
}
