// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;

/**
 * Exact private object version that contains one application secret.
 *
 * <p>The reference itself is safe to store in configuration; the secret value never is.
 */
public final class ApplicationSecretRef {
    private final String key;
    private final String version;
    private final String digest;

    /**
     * Creates a secret reference.
     *
     * @param key the private object key
     * @param version the exact object version
     * @param digest the {@code sha256:} digest of the secret object
     */
    public ApplicationSecretRef(final String key, final String version, final String digest) {
        this.key = key == null ? "" : key;
        this.version = version == null ? "" : version;
        this.digest = digest == null ? "" : digest;
    }

    /**
     * Returns the private object key.
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
     * Returns the secret object digest.
     *
     * @return the {@code sha256:} digest
     */
    public String getDigest() {
        return digest;
    }

    /**
     * Encodes the pinned reference.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("key", key);
        result.put("version", version);
        result.put("digest", digest);
        return result;
    }

    /**
     * Decodes a private reference without loading its material.
     *
     * @param value the JSON value
     * @return the reference
     * @throws IllegalArgumentException if the value is invalid
     */
    public static ApplicationSecretRef fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "application secret reference");
        ProjectConfigurationRules.requireFields(record, "application secret reference", "key", "version", "digest");
        final ApplicationSecretRef result = new ApplicationSecretRef(ProjectConfigurationRules.text(record, "key"),
                ProjectConfigurationRules.text(record, "version"), ProjectConfigurationRules.text(record, "digest"));
        if (result.version.isEmpty() || !ProjectConfigurationRules.DIGEST.matcher(result.digest).matches()) {
            throw new IllegalArgumentException("invalid application secret reference");
        }
        return result;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof ApplicationSecretRef)) {
            return false;
        }
        final ApplicationSecretRef that = (ApplicationSecretRef) other;
        return key.equals(that.key) && version.equals(that.version) && digest.equals(that.digest);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(key, version, digest);
    }
}
