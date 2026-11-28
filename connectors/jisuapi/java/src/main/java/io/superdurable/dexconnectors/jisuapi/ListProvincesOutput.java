// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jisuapi;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ListProvincesOutput {
    /** Provinces returned by the provider. */
    public final List<Province> provinces;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListProvincesOutput(@JsonProperty("provinces") List<Province> provinces) {
        this.provinces =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(provinces));
    }

    /** Metadata for one province. */
    public static final class Province {
        /** Province identifier. */
        public final String id;

        /** Province name. */
        public final String name;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Province(@JsonProperty("id") String id, @JsonProperty("name") String name) {
            this.id = id;
            this.name = name;
        }
    }
}
