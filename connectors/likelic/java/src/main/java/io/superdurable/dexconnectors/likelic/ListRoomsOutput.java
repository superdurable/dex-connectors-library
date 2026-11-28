// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.likelic;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Room page returned to Dex. */
public final class ListRoomsOutput {
    /** Visible rooms in provider order. */
    public final List<Room> rooms;

    /** Total reported by the provider. */
    public final long total;

    /** One-based page number. */
    public final long page;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListRoomsOutput(
            @JsonProperty("rooms") List<Room> rooms,
            @JsonProperty("total") long total,
            @JsonProperty("page") long page) {
        this.rooms = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(rooms));
        this.total = total;
        this.page = page;
    }

    /** Metadata for one visible room. */
    public static final class Room {
        /** Provider messaging channel. */
        public final String channel;

        /** Provider instance identifier. */
        public final long instanceID;

        /** Provider room identifier. */
        public final String roomID;

        /** Provider push status. */
        public final String pushState;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public Room(
                @JsonProperty("channel") String channel,
                @JsonProperty("instanceID") long instanceID,
                @JsonProperty("roomID") String roomID,
                @JsonProperty("pushState") String pushState) {
            this.channel = channel;
            this.instanceID = instanceID;
            this.roomID = roomID;
            this.pushState = pushState;
        }
    }
}
