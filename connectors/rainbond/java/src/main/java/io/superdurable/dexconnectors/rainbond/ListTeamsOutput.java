// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rainbond;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Authorized teams in provider order. */
public final class ListTeamsOutput {
    /** Teams returned in provider order. */
    public final List<Team> teams;

    /** Total number of teams reported by Rainbond. */
    public final long total;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListTeamsOutput(
            @JsonProperty("teams") List<Team> teams, @JsonProperty("total") long total) {
        this.teams = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(teams));
        this.total = total;
    }

    /** Metadata for one team. */
    public static final class Team {
        /** Team identifier. */
        public final String id;

        /** Team display name. */
        public final String name;

        /** Whether this team is active. */
        public final boolean active;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Team(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("active") boolean active) {
            this.id = id;
            this.name = name;
            this.active = active;
        }
    }
}
