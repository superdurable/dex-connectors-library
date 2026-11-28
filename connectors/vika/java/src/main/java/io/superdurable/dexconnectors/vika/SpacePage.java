// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.vika;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Authorized spaces in provider order. */
public final class SpacePage {
    /** Spaces returned in provider order. */
    public final List<Space> spaces;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public SpacePage(@JsonProperty("spaces") List<Space> spaces) {
        this.spaces = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(spaces));
    }

    /** Metadata for one space. */
    public static final class Space {
        /** Workspace identifier. */
        public final String id;

        /** Workspace display name. */
        public final String name;

        /** Whether the token owner administers the workspace. */
        public final boolean isAdmin;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Space(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("isAdmin") boolean isAdmin) {
            this.id = id;
            this.name = name;
            this.isAdmin = isAdmin;
        }
    }
}
