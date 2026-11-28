// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zentao;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ProductList {
    /** Products returned by the provider. */
    public final List<Product> products;

    /** Total reported by the provider. */
    public final long total;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ProductList(
            @JsonProperty("products") List<Product> products, @JsonProperty("total") long total) {
        this.products = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(products));
        this.total = total;
    }

    /** Metadata for one product. */
    public static final class Product {
        /** Product identifier. */
        public final long id;

        /** Product display name. */
        public final String name;

        /** Product code. */
        public final String code;

        /** Product type. */
        public final String type;

        /** Product access mode. */
        public final String accessControl;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Product(
                @JsonProperty("id") long id,
                @JsonProperty("name") String name,
                @JsonProperty("code") String code,
                @JsonProperty("type") String type,
                @JsonProperty("accessControl") String accessControl) {
            this.id = id;
            this.name = name;
            this.code = code;
            this.type = type;
            this.accessControl = accessControl;
        }
    }
}
