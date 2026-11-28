// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yanwen;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** The bounded warehouse catalog, in provider order. */
public final class WarehouseList {
    /** Authorized warehouses returned by this query. */
    public final List<Warehouse> warehouses;

    /** Restore a warehouse catalog from a Dex result. */
    @JsonCreator
    public WarehouseList(@JsonProperty("warehouses") List<Warehouse> warehouses) {
        this.warehouses =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(warehouses));
    }

    /** Warehouse information supplied by the provider. */
    public static final class Warehouse {
        /** Warehouse code. */
        public final String code;

        /** Warehouse display name. */
        public final String name;

        /** Warehouse area. */
        public final String area;

        /** Restore one warehouse from a Dex result. */
        @JsonCreator
        public Warehouse(
                @JsonProperty("code") String code,
                @JsonProperty("name") String name,
                @JsonProperty("area") String area) {
            this.code = code;
            this.name = name;
            this.area = area;
        }
    }
}
