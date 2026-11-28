// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.wapi;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ListVehicleBrandsOutput {
    /** Brands returned by the provider. */
    public final List<VehicleBrandPrice> brands;

    /** Total reported by the provider. */
    public final long total;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListVehicleBrandsOutput(
            @JsonProperty("brands") List<VehicleBrandPrice> brands,
            @JsonProperty("total") long total) {
        this.brands = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(brands));
        this.total = total;
    }

    /** Metadata for one vehicle brand. */
    public static final class VehicleBrandPrice {
        /** Vehicle brand identifier. */
        public final String id;

        /** Vehicle brand name. */
        public final String name;

        /** Price in fen. */
        public final long priceFen;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public VehicleBrandPrice(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("priceFen") long priceFen) {
            this.id = id;
            this.name = name;
            this.priceFen = priceFen;
        }
    }
}
