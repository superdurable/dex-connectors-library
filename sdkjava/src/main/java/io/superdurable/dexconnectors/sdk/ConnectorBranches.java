// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Branch IDs with SDK-defined meaning.
 */
public final class ConnectorBranches {
    /** The branch selected when connector code or its output violates an invariant. */
    public static final String DEFECT = "defect";
    /** The Mutation branch for a possibly accepted write that read-back cannot confirm. */
    public static final String UNCERTAIN = "uncertain";

    private ConnectorBranches() {
    }
}
