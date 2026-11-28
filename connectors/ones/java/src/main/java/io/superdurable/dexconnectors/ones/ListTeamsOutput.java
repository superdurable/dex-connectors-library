// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ones;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Authorized teams in provider order. */
public final class ListTeamsOutput {
    /** Teams returned in provider order. */
    public final List<Team> teams;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListTeamsOutput(@JsonProperty("teams") List<Team> teams) {
        this.teams = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(teams));
    }

    /** Metadata for one team. */
    public static final class Team {
        /** Team identifier. */
        public final String id;

        /** Team display name. */
        public final String name;

        /** Team status reported by ONES. */
        public final long status;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Team(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("status") long status) {
            this.id = id;
            this.name = name;
            this.status = status;
        }
    }
}
