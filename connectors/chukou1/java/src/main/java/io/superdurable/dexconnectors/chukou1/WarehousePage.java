// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.chukou1;

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

    /** Warehouse identity and country supplied by the provider. */
    public static final class Warehouse {
        /** Provider warehouse identifier. */
        public final String id;

        /** Warehouse display name. */
        public final String name;

        /** Country display name, or an empty string when absent. */
        public final String country;

        /** Provider country code. */
        public final String countryCode;

        /** Restore one warehouse from a Dex result. */
        @JsonCreator
        public Warehouse(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("country") String country,
                @JsonProperty("countryCode") String countryCode) {
            this.id = id;
            this.name = name;
            this.country = country;
            this.countryCode = countryCode;
        }
    }
}
