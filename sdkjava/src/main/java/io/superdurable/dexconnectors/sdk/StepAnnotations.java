// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.util.regex.Pattern;

/**
 * Stable group and explanation metadata for a Connector Step.
 */
public final class StepAnnotations {
    private static final Pattern GROUP_ID = Pattern.compile("^[a-z][a-z0-9-]*$");

    private final String groupId;
    private final String groupLabel;
    private final String explanation;

    /**
     * Creates validated Step annotations.
     *
     * @param groupId the stable lowercase group identifier
     * @param groupLabel the group's display label
     * @param explanation what the Step does in business terms
     * @throws IllegalArgumentException if a field is missing or the group ID is malformed
     */
    public StepAnnotations(final String groupId, final String groupLabel, final String explanation) {
        if (groupId == null || !GROUP_ID.matcher(groupId).matches()
                || groupLabel == null || groupLabel.trim().isEmpty()
                || explanation == null || explanation.trim().isEmpty()) {
            throw new IllegalArgumentException("Step group, group label, and explanation are required");
        }
        this.groupId = groupId;
        this.groupLabel = groupLabel;
        this.explanation = explanation;
    }

    /**
     * Returns the stable lowercase group identifier.
     *
     * @return the group ID
     */
    public String getGroupId() {
        return groupId;
    }

    /**
     * Returns the group's display label.
     *
     * @return the group label
     */
    public String getGroupLabel() {
        return groupLabel;
    }

    /**
     * Returns what the Step does in business terms.
     *
     * @return the explanation
     */
    public String getExplanation() {
        return explanation;
    }
}
