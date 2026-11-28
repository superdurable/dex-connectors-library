// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiban;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Tags visible to the connection. */
public final class TagsResult {
    /** Tags in provider order. */
    public final List<TagMetadata> tags;

    /** Total available tags. */
    public final long total;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public TagsResult(
            @JsonProperty("tags") List<TagMetadata> tags, @JsonProperty("total") long total) {
        this.tags = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(tags));
        this.total = total;
    }

    /** Metadata for one visible tag. */
    public static final class TagMetadata {
        /** Tag identifier. */
        public final String id;

        /** Tag display name. */
        public final String name;

        /** Provider tag group. */
        public final String group;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public TagMetadata(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("group") String group) {
            this.id = id;
            this.name = name;
            this.group = group;
        }
    }
}
