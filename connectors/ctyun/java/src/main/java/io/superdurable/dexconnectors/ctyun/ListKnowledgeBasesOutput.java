// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ctyun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/** Knowledge bases returned in provider order. */
public final class ListKnowledgeBasesOutput {
    /** Visible knowledge bases. An empty list is a successful result. */
    public final List<KnowledgeBaseSummary> knowledgeBases;

    /** Restore the list from the Dex result. */
    @JsonCreator
    public ListKnowledgeBasesOutput(
            @JsonProperty("knowledgeBases") List<KnowledgeBaseSummary> knowledgeBases) {
        this.knowledgeBases = Collections.unmodifiableList(new ArrayList<>(knowledgeBases));
    }

    /** Metadata for one CTYun knowledge base. */
    public static final class KnowledgeBaseSummary {
        /** Provider resource identifier. */
        public final long id;

        /** Provider resource name. */
        public final String name;

        /** Number of files in the knowledge base. */
        public final long fileCount;

        /** Restore resource metadata from a Dex result. */
        @JsonCreator
        public KnowledgeBaseSummary(
                @JsonProperty("id") long id,
                @JsonProperty("name") String name,
                @JsonProperty("fileCount") long fileCount) {
            this.id = id;
            this.name = name;
            this.fileCount = fileCount;
        }
    }
}
