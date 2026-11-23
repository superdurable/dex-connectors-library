// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;
import java.util.regex.Pattern;

/**
 * Trusted project and Live or Preview storage boundary.
 */
public final class Scope {
    private static final Pattern SCOPE_ID = Pattern.compile("^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$");

    private final String projectId;
    private final String kind;

    /**
     * Creates a scope; {@link #prefix()} validates it.
     *
     * @param projectId the project ID
     * @param kind {@code live} or {@code preview}
     */
    public Scope(final String projectId, final String kind) {
        this.projectId = projectId == null ? "" : projectId;
        this.kind = kind == null ? "" : kind;
    }

    /**
     * Returns the canonical scoped object key prefix after validation.
     *
     * @return {@code projects/<projectId>/<kind>}
     * @throws IllegalArgumentException if the scope is invalid
     */
    public String prefix() {
        if (!SCOPE_ID.matcher(projectId).matches() || !("live".equals(kind) || "preview".equals(kind))) {
            throw new IllegalArgumentException("invalid project configuration scope");
        }
        return "projects/" + projectId + "/" + kind;
    }

    /**
     * Returns the project ID.
     *
     * @return the project ID
     */
    public String getProjectId() {
        return projectId;
    }

    /**
     * Returns the scope kind.
     *
     * @return {@code live} or {@code preview}
     */
    public String getKind() {
        return kind;
    }

    /**
     * Encodes the Go scope shape.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        prefix();
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("projectId", projectId);
        result.put("kind", kind);
        return result;
    }

    /**
     * Decodes and validates a scope object.
     *
     * @param value the JSON value
     * @return the scope
     * @throws IllegalArgumentException if the value is invalid
     */
    public static Scope fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "scope");
        ProjectConfigurationRules.requireFields(record, "scope", "projectId", "kind");
        final Scope result = new Scope(ProjectConfigurationRules.text(record, "projectId"),
                ProjectConfigurationRules.text(record, "kind"));
        result.prefix();
        return result;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof Scope)) {
            return false;
        }
        final Scope that = (Scope) other;
        return projectId.equals(that.projectId) && kind.equals(that.kind);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(projectId, kind);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return projectId + "/" + kind;
    }
}
