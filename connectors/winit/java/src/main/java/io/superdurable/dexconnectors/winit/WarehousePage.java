// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.winit;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** The bounded warehouse catalog, in provider order. */
public final class WarehousePage {
    /** Authorized warehouses returned by this query. */
    public final List<Warehouse> warehouses;

    /** Restore a warehouse catalog from a Dex result. */
    @JsonCreator
    public WarehousePage(@JsonProperty("warehouses") List<Warehouse> warehouses) {
        this.warehouses =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(warehouses));
    }

    /** Warehouse information supplied by the provider. */
    public static final class Warehouse {
        /** Warehouse identifier. */
        public final long id;

        /** Warehouse code. */
        public final String code;

        /** Warehouse display name. */
        public final String name;

        /** Restore one warehouse from a Dex result. */
        @JsonCreator
        public Warehouse(
                @JsonProperty("id") long id,
                @JsonProperty("code") String code,
                @JsonProperty("name") String name) {
            this.id = id;
            this.code = code;
            this.name = name;
        }
    }
}
