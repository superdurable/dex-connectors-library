// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.time.Instant;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

/**
 * Reads ordinary credential heads and immutable private material, and binds loaded shared
 * connections for provider routing.
 *
 * <p>Shared connections are constructor-injected from the pinned project document. Their
 * credentials stay broker-owned and never enter stored connection heads.
 */
public final class ConnectionStore {
    private static final Set<String> RECORD_FIELDS = new HashSet<>(Arrays.asList("schemaVersion", "connection",
            "scope", "revision", "fence", "status", "expiresAt", "authMethod", "credential", "mutation"));
    private static final Set<String> STATUSES = new HashSet<>(
            Arrays.asList("READY", "REFRESHING", "AUTHORIZING", "REAUTHORIZATION_REQUIRED", "REVOKED"));
    private static final Pattern DIGEST_HEX = Pattern.compile("^[0-9a-f]{64}$");
    private static final Pattern ATTEMPT_ID = Pattern.compile("^[A-Za-z0-9_-]{1,128}$");

    private final ObjectStore objects;
    private final Scope scope;
    private final Map<ConnectionKey, SharedConnection> sharedConnections;

    /**
     * Binds ordinary connections of one scope.
     *
     * @param objects the project object store
     * @param scope the project scope
     */
    public ConnectionStore(final ObjectStore objects, final Scope scope) {
        this(objects, scope, Collections.<ConnectionKey, SharedConnection>emptyMap());
    }

    /**
     * Binds ordinary and broker-backed shared connections of one scope.
     *
     * @param objects the project object store
     * @param scope the project scope
     * @param sharedConnections the shared connections by key
     */
    public ConnectionStore(
            final ObjectStore objects, final Scope scope, final Map<ConnectionKey, SharedConnection> sharedConnections) {
        if (objects == null) {
            throw new IllegalArgumentException("project connection object store is required");
        }
        this.objects = objects;
        this.scope = scope;
        this.sharedConnections = new HashMap<>(sharedConnections == null
                ? Collections.<ConnectionKey, SharedConnection>emptyMap() : sharedConnections);
    }

    /**
     * Returns the object store.
     *
     * @return the store
     */
    public ObjectStore getObjects() {
        return objects;
    }

    /**
     * Returns the scope.
     *
     * @return the scope
     */
    public Scope getScope() {
        return scope;
    }

    SharedConnection sharedConnection(final ConnectionKey key) {
        return sharedConnections.get(key);
    }

    /**
     * Reads safe metadata without refreshing credentials.
     *
     * @param key the connection key
     * @return the metadata
     * @throws IOException if storage fails or the head is absent
     */
    public ProjectConnection readConnection(final ConnectionKey key) throws IOException {
        return connection(key, readRecord(key).record);
    }

    /**
     * Reads exact private material for trusted provider boundaries.
     *
     * @param key the connection key
     * @return the material and its metadata
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if the credential is unavailable or its integrity differs
     */
    public LoadedMaterial readCredentialMaterial(final ConnectionKey key) throws IOException {
        final HeadRecord head = readRecord(key);
        final ProjectConnection connection = connection(key, head.record);
        if (!"READY".equals(connection.getStatus())) {
            throw new IllegalArgumentException("project credential is unavailable");
        }
        return new LoadedMaterial(readMaterial(key, head.record), connection);
    }

    /**
     * Fences earlier work and publishes a private immutable credential version.
     *
     * @param key the connection key
     * @param expectedRevision the current head revision, or 0 for a new connection
     * @param material the complete replacement material
     * @return the new metadata
     * @throws ObjectConflictException if the revision differs
     * @throws IOException if storage fails
     */
    public ProjectConnection replaceCredential(
            final ConnectionKey key, final long expectedRevision, final CredentialMaterial material) throws IOException {
        if (material == null || material.getAuthMethod().isEmpty()) {
            throw new IllegalArgumentException("invalid credential material");
        }
        final String path = key.path(scope);
        long revision = 0;
        long fence = 0;
        ProjectObject head = null;
        try {
            final HeadRecord current = readRecord(key);
            revision = current.record.get("revision").longValue();
            fence = current.record.get("fence").longValue();
            head = current.object;
        } catch (ObjectNotFoundException error) {
            if (expectedRevision != 0) {
                throw error;
            }
        }
        if (revision != expectedRevision) {
            throw new ObjectConflictException("connection revision conflict");
        }
        final long nextFence = fence + 1;
        final byte[] contents = GoCompatibleJson.encode(ConnectionDocuments.credentialEnvelope(scope, key, nextFence,
                material.getCredentials(), material.getExpiresAt(), material.getAuthMethod(), ""));
        final String digest = GoCompatibleJson.sha256Hex(contents);
        final String immutableKey = path + "/credentials/" + digest;
        ProjectObject candidate;
        try {
            candidate = objects.createObject(immutableKey, contents);
        } catch (ObjectConflictException | OutcomeUnknownException error) {
            candidate = objects.readObject(immutableKey, "");
            if (!candidate.hasContents(contents)) {
                throw new IllegalArgumentException("immutable credential content differs");
            }
        }
        final ObjectNode reference = GoCompatibleJson.newObject();
        reference.put("key", immutableKey);
        reference.put("version", candidate.getVersion());
        reference.put("digest", digest);
        final ObjectNode record = ConnectionDocuments.connectionRecord(scope, key, expectedRevision + 1, nextFence,
                "READY", material.getExpiresAt(), material.getAuthMethod(), reference, null);
        final byte[] headContents = GoCompatibleJson.encode(record);
        try {
            if (head == null) {
                objects.createObject(path + "/head", headContents);
            } else {
                objects.compareAndSwapObject(path + "/head", head.getEtag(), headContents);
            }
        } catch (OutcomeUnknownException unknown) {
            final ProjectObject observed = objects.readObject(path + "/head", "");
            if (!observed.hasContents(headContents)) {
                throw unknown;
            }
        }
        return connection(key, record);
    }

    CredentialMaterial readMaterial(final ConnectionKey key, final ObjectNode record) throws IOException {
        final ObjectNode reference = GoCompatibleJson.requireObject(record.get("credential"), "credential reference");
        final ProjectObject item = objects.readObject(ProjectConfigurationRules.text(reference, "key"),
                ProjectConfigurationRules.text(reference, "version"));
        if (!GoCompatibleJson.sha256Hex(item.getContents()).equals(ProjectConfigurationRules.text(reference, "digest"))) {
            throw new IllegalArgumentException("project credential integrity differs");
        }
        final ObjectNode envelope = GoCompatibleJson.parseObject(item.getContents(), "credential envelope");
        if (!ConnectionDocuments.CONNECTION_SCHEMA_VERSION.equals(ProjectConfigurationRules.text(envelope, "schemaVersion"))
                || !scope.toJson().equals(envelope.get("scope")) || !key.toJson().equals(envelope.get("connection"))
                || !nullableEquals(envelope.get("authMethod"), record.get("authMethod"))
                || !nullableEquals(envelope.get("expiresAt"), record.get("expiresAt"))) {
            throw new IllegalArgumentException("project credential identity differs");
        }
        final JsonNode expiresAt = envelope.get("expiresAt");
        return new CredentialMaterial(GoCompatibleJson.requireObject(envelope.get("credentials"), "credentials"),
                expiresAt == null || expiresAt.isNull() ? null : Rfc3339NanoTimestamp.fromWire(expiresAt.asText()),
                ProjectConfigurationRules.text(envelope, "authMethod"));
    }

    HeadRecord readRecord(final ConnectionKey key) throws IOException {
        final String path = key.path(scope);
        final ProjectObject item = objects.readObject(path + "/head", "");
        final ObjectNode record = GoCompatibleJson.parseObject(item.getContents(), "connection head");
        final Iterator<String> names = record.fieldNames();
        boolean hasOnlyKnownFields = true;
        while (names.hasNext()) {
            hasOnlyKnownFields = hasOnlyKnownFields && RECORD_FIELDS.contains(names.next());
        }
        final JsonNode revision = record.get("revision");
        final JsonNode fence = record.get("fence");
        final String status = ProjectConfigurationRules.text(record, "status");
        if (item.getVersion().isEmpty() || item.getEtag().isEmpty() || !hasOnlyKnownFields
                || !ConnectionDocuments.CONNECTION_SCHEMA_VERSION.equals(ProjectConfigurationRules.text(record, "schemaVersion"))
                || !scope.toJson().equals(record.get("scope")) || !key.toJson().equals(record.get("connection"))
                || revision == null || !revision.isIntegralNumber() || revision.longValue() <= 0
                || fence == null || !fence.isIntegralNumber() || fence.longValue() <= 0
                || !STATUSES.contains(status)) {
            throw new IllegalArgumentException("project connection identity is invalid");
        }
        final JsonNode reference = record.get("credential");
        final boolean hasReference = reference != null && !reference.isNull();
        final JsonNode mutation = record.get("mutation");
        final boolean hasMutation = mutation != null && !mutation.isNull();
        if (("READY".equals(status) || "REFRESHING".equals(status)) && !hasReference) {
            throw new IllegalArgumentException("project credential reference is absent");
        }
        if ("REVOKED".equals(status) && (hasReference || hasMutation)) {
            throw new IllegalArgumentException("revoked connection retains active material");
        }
        if (hasReference) {
            final ObjectNode credential = GoCompatibleJson.requireObject(reference, "credential reference");
            final JsonNode version = credential.get("version");
            final JsonNode digest = credential.get("digest");
            if (!ProjectConfigurationRules.text(credential, "key").startsWith(path + "/")
                    || version == null || !version.isTextual() || version.textValue().isEmpty()
                    || digest == null || !digest.isTextual() || !DIGEST_HEX.matcher(digest.textValue()).matches()) {
                throw new IllegalArgumentException("project credential reference is invalid");
            }
        }
        if ("REFRESHING".equals(status) || "AUTHORIZING".equals(status)) {
            if (!hasMutation || !mutation.isObject()) {
                throw new IllegalArgumentException("credential exchange admission is absent");
            }
            final JsonNode attemptId = mutation.get("id");
            final JsonNode invocationId = mutation.get("invocationId");
            if (attemptId == null || !attemptId.isTextual() || !ATTEMPT_ID.matcher(attemptId.textValue()).matches()
                    || !status.equals(ProjectConfigurationRules.text(mutation, "kind"))
                    || invocationId == null || !invocationId.isTextual() || invocationId.textValue().isEmpty()
                    || !isInteger(mutation.get("fence"), fence.longValue())
                    || !isInteger(mutation.get("baseRevision"), revision.longValue() - 1)
                    || !(path + "/exchange-results/" + attemptId.textValue())
                            .equals(ProjectConfigurationRules.text(mutation, "resultKey"))
                    || parseTime(mutation.get("deadline")) == null) {
                throw new IllegalArgumentException("credential exchange admission is invalid");
            }
        } else if (hasMutation) {
            throw new IllegalArgumentException("inactive credential exchange admission is invalid");
        }
        return new HeadRecord(record, item);
    }

    ProjectConnection connection(final ConnectionKey key, final ObjectNode record) {
        return new ProjectConnection(key, scope, record.get("revision").longValue(), record.get("fence").longValue(),
                ProjectConfigurationRules.text(record, "status"), parseTime(record.get("expiresAt")),
                ProjectConfigurationRules.text(record, "authMethod"));
    }

    static Instant parseTime(final JsonNode value) {
        if (value == null || value.isNull()) {
            return null;
        }
        if (!value.isTextual()) {
            throw new IllegalArgumentException("invalid credential expiry");
        }
        return Rfc3339NanoTimestamp.fromWire(value.textValue()).toOffsetDateTime().toInstant();
    }

    private static boolean isInteger(final JsonNode value, final long expected) {
        return value != null && value.isIntegralNumber() && value.longValue() == expected;
    }

    private static boolean nullableEquals(final JsonNode left, final JsonNode right) {
        final boolean isLeftAbsent = left == null || left.isNull();
        final boolean isRightAbsent = right == null || right.isNull();
        if (isLeftAbsent || isRightAbsent) {
            return isLeftAbsent && isRightAbsent;
        }
        return left.equals(right);
    }

    /**
     * Private credential material read together with its connection metadata.
     */
    public static final class LoadedMaterial {
        private final CredentialMaterial material;
        private final ProjectConnection connection;

        LoadedMaterial(final CredentialMaterial material, final ProjectConnection connection) {
            this.material = material;
            this.connection = connection;
        }

        /**
         * Returns the private material.
         *
         * @return the material
         */
        public CredentialMaterial getMaterial() {
            return material;
        }

        /**
         * Returns the connection metadata.
         *
         * @return the metadata
         */
        public ProjectConnection getConnection() {
            return connection;
        }
    }

    static final class HeadRecord {
        final ObjectNode record;
        final ProjectObject object;

        HeadRecord(final ObjectNode record, final ProjectObject object) {
            this.record = record;
            this.object = object;
        }
    }
}
