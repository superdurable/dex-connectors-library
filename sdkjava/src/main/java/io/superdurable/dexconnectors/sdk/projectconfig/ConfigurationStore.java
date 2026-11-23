// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.security.SecureRandom;

/**
 * Revision compare-and-swap and exact-version reads of the ordinary configuration document.
 */
public final class ConfigurationStore {
    private static final SecureRandom RANDOM = new SecureRandom();

    private final ObjectStore objects;
    private final Scope scope;
    private final String key;

    /**
     * Binds the configuration head of one scope.
     *
     * @param objects the project object store
     * @param scope the project scope
     */
    public ConfigurationStore(final ObjectStore objects, final Scope scope) {
        if (objects == null) {
            throw new IllegalArgumentException("configuration object store is required");
        }
        this.objects = objects;
        this.scope = scope;
        this.key = scope.prefix() + "/configuration/head";
    }

    /**
     * Returns the scope.
     *
     * @return the scope
     */
    public Scope getScope() {
        return scope;
    }

    /**
     * Reads and validates the current configuration document.
     *
     * @return the current document and its object
     * @throws IOException if storage fails or the head is absent
     */
    public VersionedConfiguration readConfiguration() throws IOException {
        final ProjectObject item = objects.readObject(key, "");
        return new VersionedConfiguration(decode(item), item);
    }

    /**
     * Conditionally replaces ordinary settings when the stored revision equals the expected one.
     *
     * <p>The written document has revision {@code expectedRevision + 1} and the previous schema
     * unless it declares a broker or a shared connection.
     *
     * @param expectedRevision the current revision, or 0 for a new head
     * @param configuration the settings to write; its revision and schema are replaced
     * @return the written document and object
     * @throws ObjectConflictException if the revision differs
     * @throws IOException if storage fails
     */
    public VersionedConfiguration writeConfiguration(final long expectedRevision, final Configuration configuration)
            throws IOException {
        VersionedConfiguration previous = null;
        try {
            previous = readConfiguration();
        } catch (ObjectNotFoundException error) {
            if (expectedRevision != 0) {
                throw error;
            }
        }
        if ((previous == null ? 0 : previous.getConfiguration().getRevision()) != expectedRevision) {
            throw new ObjectConflictException("configuration revision conflict");
        }
        boolean hasSharedConnection = false;
        for (ConnectionConfiguration entry : configuration.getConnections()) {
            hasSharedConnection = hasSharedConnection || !entry.getSharedConnectionId().isEmpty();
        }
        final String schemaVersion = configuration.getCredentialBroker() == null && !hasSharedConnection
                ? Configuration.PREVIOUS_SCHEMA_VERSION
                : Configuration.SCHEMA_VERSION;
        final Configuration.Builder builder = Configuration.newBuilder(scope, expectedRevision + 1)
                .credentialBroker(configuration.getCredentialBroker())
                .schemaVersion(schemaVersion);
        for (ConnectionConfiguration entry : configuration.getConnections()) {
            builder.addConnection(entry);
        }
        for (ObjectNode binding : configuration.getTriggerBindings()) {
            builder.addTriggerBinding(binding);
        }
        for (ObjectNode operation : configuration.getOperationConfigurations()) {
            builder.addOperationConfiguration(operation);
        }
        for (java.util.Map.Entry<String, EnvironmentValue> entry : configuration.getEnvironment().entrySet()) {
            builder.putEnvironment(entry.getKey(), entry.getValue());
        }
        final Configuration updated = builder.build();
        final byte[] contents = GoCompatibleJson.encode(updated.toJson());
        ProjectObject item;
        try {
            item = previous == null
                    ? objects.createObject(key, contents)
                    : objects.compareAndSwapObject(key, previous.getObject().getEtag(), contents);
        } catch (OutcomeUnknownException unknown) {
            final ProjectObject observed = objects.readObject(key, "");
            if (!observed.hasContents(contents)) {
                throw unknown;
            }
            item = observed;
        }
        return new VersionedConfiguration(updated, item);
    }

    /**
     * Pins the current exact version when the revision matches.
     *
     * @param expectedRevision the revision to pin
     * @return the snapshot reference
     * @throws ObjectConflictException if the revision differs
     * @throws IOException if storage fails
     */
    public SnapshotRef freezeConfiguration(final long expectedRevision) throws IOException {
        final VersionedConfiguration current = readConfiguration();
        if (current.getConfiguration().getRevision() != expectedRevision) {
            throw new ObjectConflictException("configuration revision conflict");
        }
        final ProjectObject item = current.getObject();
        return new SnapshotRef(item.getKey(), item.getVersion(), GoCompatibleJson.digestBytes(item.getContents()));
    }

    /**
     * Reads a pinned snapshot after verifying scope, version, and the full document digest.
     *
     * @param reference the snapshot reference
     * @return the configuration
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if the reference or digest differs
     */
    public Configuration readSnapshot(final SnapshotRef reference) throws IOException {
        reference.validate(scope);
        final ProjectObject item = objects.readObject(reference.getKey(), reference.getVersion());
        if (!GoCompatibleJson.digestBytes(item.getContents()).equals(reference.getDigest())) {
            throw new IllegalArgumentException("configuration snapshot digest differs");
        }
        return decode(item);
    }

    /**
     * Writes one immutable private value beneath this scope and environment name.
     *
     * @param name the application environment name
     * @param value the secret value
     * @return the pinned reference
     * @throws IOException if storage fails
     */
    public ApplicationSecretRef createApplicationSecret(final String name, final String value) throws IOException {
        if (!ProjectConfigurationRules.isApplicationEnvironmentName(name)) {
            throw new IllegalArgumentException("invalid application secret name");
        }
        ProjectConfigurationRules.validateEnvironmentValue(value);
        return createSecret(name, value);
    }

    /**
     * Writes one immutable broker scope token outside application environment names.
     *
     * @param token the scope token
     * @return the pinned reference
     * @throws IOException if storage fails
     */
    public ApplicationSecretRef createCredentialBrokerToken(final String token) throws IOException {
        if (token == null || token.isEmpty()) {
            throw new IllegalArgumentException("credential broker token is required");
        }
        ProjectConfigurationRules.validateEnvironmentValue(token);
        return createSecret(ProjectConfigurationRules.CREDENTIAL_BROKER_SECRET_NAME, token);
    }

    /**
     * Reads exact private bytes and verifies digest, scope, and field identity.
     *
     * @param reference the pinned reference
     * @param name the environment or reserved secret name
     * @return the secret value
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if integrity or identity differs
     */
    public String readSecretValue(final ApplicationSecretRef reference, final String name) throws IOException {
        ProjectConfigurationRules.validateSecretReference(reference, scope, name);
        final ProjectObject item = objects.readObject(reference.getKey(), reference.getVersion());
        if (!item.getVersion().equals(reference.getVersion())
                || !GoCompatibleJson.digestBytes(item.getContents()).equals(reference.getDigest())) {
            throw new IllegalArgumentException("application secret integrity differs");
        }
        final ObjectNode record = GoCompatibleJson.parseObject(item.getContents(), "application secret");
        final JsonNode value = record.get("value");
        if (!scope.toJson().equals(record.get("scope")) || record.get("name") == null
                || !name.equals(record.get("name").textValue()) || value == null || !value.isTextual()) {
            throw new IllegalArgumentException("application secret scope or identity differs");
        }
        ProjectConfigurationRules.validateEnvironmentValue(value.textValue());
        return value.textValue();
    }

    private ApplicationSecretRef createSecret(final String name, final String value) throws IOException {
        final byte[] identifier = new byte[16];
        RANDOM.nextBytes(identifier);
        final StringBuilder hex = new StringBuilder();
        for (byte part : identifier) {
            hex.append(String.format("%02x", part & 0xff));
        }
        final String secretKey = scope.prefix() + "/app-secrets/" + name + "/" + hex;
        final ObjectNode document = GoCompatibleJson.newObject();
        document.set("scope", scope.toJson());
        document.put("name", name);
        document.put("value", value);
        final byte[] contents = GoCompatibleJson.encode(document);
        ProjectObject item;
        try {
            item = objects.createObject(secretKey, contents);
        } catch (ObjectConflictException | OutcomeUnknownException error) {
            item = objects.readObject(secretKey, "");
            if (!item.hasContents(contents)) {
                throw new IllegalArgumentException("application secret content differs");
            }
        }
        return new ApplicationSecretRef(secretKey, item.getVersion(), GoCompatibleJson.digestBytes(contents));
    }

    private Configuration decode(final ProjectObject item) {
        if (!item.getKey().equals(key) || item.getVersion().isEmpty() || item.getEtag().isEmpty()) {
            throw new IllegalArgumentException("configuration document identity is invalid");
        }
        final Configuration configuration = Configuration.fromJson(GoCompatibleJson.parse(item.getContents()));
        if (!configuration.getScope().equals(scope)) {
            throw new IllegalArgumentException("configuration document scope differs");
        }
        return configuration;
    }

    /**
     * A configuration document with the object that stores it.
     */
    public static final class VersionedConfiguration {
        private final Configuration configuration;
        private final ProjectObject object;

        VersionedConfiguration(final Configuration configuration, final ProjectObject object) {
            this.configuration = configuration;
            this.object = object;
        }

        /**
         * Returns the configuration.
         *
         * @return the configuration
         */
        public Configuration getConfiguration() {
            return configuration;
        }

        /**
         * Returns the stored object.
         *
         * @return the object
         */
        public ProjectObject getObject() {
            return object;
        }
    }
}
