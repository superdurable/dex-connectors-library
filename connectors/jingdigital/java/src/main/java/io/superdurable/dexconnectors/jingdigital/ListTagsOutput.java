// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jingdigital;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Tags visible to the connection. */
public final class ListTagsOutput {
    /** Tags in provider order. */
    public final List<TagSummary> tags;

    /** Total available tags. */
    public final long total;

    /** Next page, or zero after the last page. */
    public final long nextPage;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListTagsOutput(
            @JsonProperty("tags") List<TagSummary> tags,
            @JsonProperty("total") long total,
            @JsonProperty("nextPage") long nextPage) {
        this.tags = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(tags));
        this.total = total;
        this.nextPage = nextPage;
    }

    /** Metadata for one visible tag. */
    public static final class TagSummary {
        /** Tag identifier. */
        public final long id;

        /** Tag display name. */
        public final String name;

        /** Provider tag level. */
        public final long level;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public TagSummary(
                @JsonProperty("id") long id,
                @JsonProperty("name") String name,
                @JsonProperty("level") long level) {
            this.id = id;
            this.name = name;
            this.level = level;
        }
    }
}
