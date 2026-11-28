// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zilliz;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ListClustersOutput {
    /** Clusters returned by the provider. */
    public final List<ClusterSummary> clusters;

    /** Total clusters available to the caller. */
    public final long total;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListClustersOutput(
            @JsonProperty("clusters") List<ClusterSummary> clusters,
            @JsonProperty("total") long total) {
        this.clusters = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(clusters));
        this.total = total;
    }

    /** Metadata for one cluster. */
    public static final class ClusterSummary {
        /** Cluster identifier. */
        public final String clusterID;

        /** Cluster display name. */
        public final String name;

        /** Deployment region identifier. */
        public final String regionID;

        /** Service plan. */
        public final String plan;

        /** Provider status. */
        public final String status;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public ClusterSummary(
                @JsonProperty("clusterID") String clusterID,
                @JsonProperty("name") String name,
                @JsonProperty("regionID") String regionID,
                @JsonProperty("plan") String plan,
                @JsonProperty("status") String status) {
            this.clusterID = clusterID;
            this.name = name;
            this.regionID = regionID;
            this.plan = plan;
            this.status = status;
        }
    }
}
