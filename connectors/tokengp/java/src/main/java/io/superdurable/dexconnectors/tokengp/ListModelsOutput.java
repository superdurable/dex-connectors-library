// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tokengp;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Predicate;

/** TokenGP models in the order returned by the authorized catalog. */
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
        /** Restore one model from its Dex wire representation. */
        @JsonCreator public Model(@JsonProperty("id") String id) {
            this.id = id;
        }
    }

    static ListModelsOutput empty() { return new ListModelsOutput(Collections.emptyList()); }

    static ListModelsOutput fromProviderModels(JsonNode rows, Predicate<String> containsCredential) {
        if (rows.size() > 10000) return null;
        List<Model> models = new ArrayList<>(rows.size());
        for (JsonNode row : rows) {
            String id = row.path("id").textValue();
            if (id == null || id.isEmpty() || containsCredential.test(id)) return null;
            if (id.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 256 ||
                    isGoSpace(id.codePointAt(0)) || isGoSpace(id.codePointBefore(id.length()))) return null;
            models.add(new Model(id));
        }
        return new ListModelsOutput(models);
    }

    private static boolean isGoSpace(int codePoint) {
        return (codePoint >= 9 && codePoint <= 13) || codePoint == 0x85 || Character.isSpaceChar(codePoint);
    }
}
