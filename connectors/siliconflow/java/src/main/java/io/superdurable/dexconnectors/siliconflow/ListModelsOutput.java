// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.siliconflow;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Set;
import java.util.HashSet;
import java.util.function.Predicate;

/** SiliconFlow models in the order returned by the authorized catalog. */
public final class ListModelsOutput {
    /** Models accessible to the connection in provider order. */
    public final List<Model> models;

    /** Restore the complete ordered catalog from Dex. */
    @JsonCreator public ListModelsOutput(@JsonProperty("models") List<Model> models) {
        this.models = Collections.unmodifiableList(new ArrayList<>(models));
    }

    /** Immutable provider metadata for a single model. */
    public static final class Model {
        /** Model identifier accepted by the inference API. */
        public final String id;
        /** Provider object discriminator, omitted when empty. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String object;

        /** Restore one model from its Dex wire representation. */
        @JsonCreator public Model(@JsonProperty("id") String id, @JsonProperty("object") String object) {
            this.id = id;
            this.object = object == null ? "" : object;
        }
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode rows, Predicate<String> containsCredential) {
        if (rows.size() > 10000) return null;
        Set<String> seen = new HashSet<>();
        List<Model> models = new ArrayList<>(rows.size());
        for (JsonNode row : rows) {
            String id = row.path("id").textValue();
            if (id == null || id.isEmpty() || containsCredential.test(id)) return null;
            if (id.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 512 || !seen.add(id)) return null;
            JsonNode tag = row.get("object");
            if (tag != null && !tag.isNull() && (!tag.isTextual() || containsCredential.test(tag.textValue()))) return null;
            models.add(new Model(id, tag == null ? "" : tag.asText("")));
        }
        return new ListModelsOutput(models);
    }
}
