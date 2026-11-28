// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.baichuan;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/** Knowledge bases returned in provider order. */
public final class ListKnowledgeBasesOutput {
    /** Visible knowledge bases. An empty list is a successful result. */
    public final List<KnowledgeBase> knowledgeBases;

    /** Restore the list from the Dex result. */
    @JsonCreator
    public ListKnowledgeBasesOutput(
            @JsonProperty("knowledgeBases") List<KnowledgeBase> knowledgeBases) {
        this.knowledgeBases = Collections.unmodifiableList(new ArrayList<>(knowledgeBases));
    }

    /** Metadata for one Baichuan knowledge base. */
    public static final class KnowledgeBase {
        /** Stable knowledge-base identifier. */
        public final String id;

        /** Provider resource name. */
        public final String name;

        /** Provider description. */
        public final String description;

        /** Provider processing status. */
        public final String status;

        /** Attached provider file identifiers. */
        public final List<String> fileIds;

        /** Creation time in Unix seconds. */
        public final long createdAt;

        /** Last update time in Unix seconds. */
        public final long updatedAt;

        /** Restore resource metadata from a Dex result. */
        @JsonCreator
        public KnowledgeBase(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("description") String description,
                @JsonProperty("status") String status,
                @JsonProperty("fileIds") List<String> fileIds,
                @JsonProperty("createdAt") long createdAt,
                @JsonProperty("updatedAt") long updatedAt) {
            this.id = id;
            this.name = name;
            this.description = description;
            this.status = status;
            this.fileIds = Collections.unmodifiableList(new ArrayList<>(fileIds));
            this.createdAt = createdAt;
            this.updatedAt = updatedAt;
        }
    }
}
