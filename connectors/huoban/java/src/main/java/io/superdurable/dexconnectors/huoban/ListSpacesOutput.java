// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.huoban;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Authorized spaces in provider order. */
public final class ListSpacesOutput {
    /** Spaces returned in provider order. */
    public final List<SpaceMetadata> spaces;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListSpacesOutput(@JsonProperty("spaces") List<SpaceMetadata> spaces) {
        this.spaces = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(spaces));
    }

    /** Metadata for one space. */
    public static final class SpaceMetadata {
        /** Workspace identifier. */
        public final String spaceId;

        /** Workspace display name. */
        public final String name;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public SpaceMetadata(
                @JsonProperty("spaceId") String spaceId, @JsonProperty("name") String name) {
            this.spaceId = spaceId;
            this.name = name;
        }
    }
}
