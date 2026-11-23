// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;

/**
 * Exact version and digest of an immutable ordinary configuration document.
 */
public final class SnapshotRef {
    /** The only supported snapshot media type. */
    public static final String MEDIA_TYPE = "application/json";

    private final String key;
    private final String version;
    private final String digest;
    private final String mediaType;

    /**
     * Creates a JSON snapshot reference.
     *
     * @param key the configuration head key
     * @param version the exact object version
     * @param digest the {@code sha256:} document digest
     */
    public SnapshotRef(final String key, final String version, final String digest) {
        this(key, version, digest, MEDIA_TYPE);
    }

    /**
     * Creates a snapshot reference.
     *
     * @param key the configuration head key
     * @param version the exact object version
     * @param digest the {@code sha256:} document digest
     * @param mediaType the media type
     */
    public SnapshotRef(final String key, final String version, final String digest, final String mediaType) {
        this.key = key == null ? "" : key;
        this.version = version == null ? "" : version;
        this.digest = digest == null ? "" : digest;
        this.mediaType = mediaType == null ? "" : mediaType;
    }

    /**
     * Requires this scope's head, an exact version, and a SHA-256 digest.
     *
     * @param scope the project scope
     * @throws IllegalArgumentException if the reference is invalid
     */
    public void validate(final Scope scope) {
        if (!key.equals(scope.prefix() + "/configuration/head") || version.isEmpty() || "null".equals(version)
                || !ProjectConfigurationRules.DIGEST.matcher(digest).matches() || !MEDIA_TYPE.equals(mediaType)) {
            throw new IllegalArgumentException("invalid configuration snapshot reference");
        }
    }

    /**
     * Returns the configuration head key.
     *
     * @return the key
     */
    public String getKey() {
        return key;
    }

    /**
     * Returns the exact object version.
     *
     * @return the version
     */
    public String getVersion() {
        return version;
    }

    /**
     * Returns the document digest.
     *
     * @return the {@code sha256:} digest
     */
    public String getDigest() {
        return digest;
    }

    /**
     * Returns the media type.
     *
     * @return the media type
     */
    public String getMediaType() {
        return mediaType;
    }

    /**
     * Encodes the Go snapshot reference shape.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("key", key);
        result.put("version", version);
        result.put("digest", digest);
        result.put("mediaType", mediaType);
        return result;
    }

    /**
     * Decodes a complete exact-version configuration reference.
     *
     * @param value the JSON value
     * @return the reference
     * @throws IllegalArgumentException if the value has unknown fields
     */
    public static SnapshotRef fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "snapshot");
        ProjectConfigurationRules.requireFields(record, "snapshot", "key", "version", "digest", "mediaType");
        return new SnapshotRef(ProjectConfigurationRules.text(record, "key"),
                ProjectConfigurationRules.text(record, "version"), ProjectConfigurationRules.text(record, "digest"),
                ProjectConfigurationRules.text(record, "mediaType"));
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof SnapshotRef)) {
            return false;
        }
        final SnapshotRef that = (SnapshotRef) other;
        return key.equals(that.key) && version.equals(that.version) && digest.equals(that.digest)
                && mediaType.equals(that.mediaType);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(key, version, digest, mediaType);
    }
}
