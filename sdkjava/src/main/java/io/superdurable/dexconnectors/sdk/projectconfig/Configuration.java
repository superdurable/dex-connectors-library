// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.TreeMap;
import java.util.regex.Pattern;

/**
 * Revisioned, secret-free project configuration snapshot shared with Dex Web and Go.
 *
 * <p>Instances are immutable. Build one with {@link #newBuilder(Scope, long)}; {@link #validate()}
 * rejects duplicate identities, unsupported schemas, and a shared connection without a broker.
 */
public final class Configuration {
    /** Current project configuration schema. */
    public static final String SCHEMA_VERSION = "connectors.dex.dev/project-configuration/v1alpha3";
    /** Previous project configuration schema, without a credential broker. */
    public static final String PREVIOUS_SCHEMA_VERSION = "connectors.dex.dev/project-configuration/v1alpha2";
    /** Dex Web {@code connections.json} schema. */
    public static final String LOCAL_CONNECTIONS_SCHEMA_VERSION = "connectors.dex.dev/local-connections/v1alpha1";
    /** Dex Web {@code use-configurations.json} schema. */
    public static final String LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION =
            "connectors.dex.dev/local-use-configurations/v1alpha1";

    private static final Pattern SHARED_ID = Pattern.compile("^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$");

    private final Scope scope;
    private final long revision;
    private final List<ConnectionConfiguration> connections;
    private final List<ObjectNode> triggerBindings;
    private final List<ObjectNode> operationConfigurations;
    private final Map<String, EnvironmentValue> environment;
    private final CredentialBrokerConfiguration credentialBroker;
    private final String schemaVersion;

    private Configuration(final Builder builder) {
        this.scope = Objects.requireNonNull(builder.scope, "scope");
        this.revision = builder.revision;
        this.connections = Collections.unmodifiableList(new ArrayList<>(builder.connections));
        this.triggerBindings = copyObjects(builder.triggerBindings);
        this.operationConfigurations = copyObjects(builder.operationConfigurations);
        this.environment = Collections.unmodifiableMap(new LinkedHashMap<>(builder.environment));
        this.credentialBroker = builder.credentialBroker;
        this.schemaVersion = builder.schemaVersion;
    }

    /**
     * Starts a configuration with the current schema and no entries.
     *
     * @param scope the project scope
     * @param revision the positive document revision
     * @return a new builder
     */
    public static Builder newBuilder(final Scope scope, final long revision) {
        return new Builder(scope, revision);
    }

    /**
     * Returns a builder initialized from this configuration.
     *
     * @return a new builder
     */
    public Builder toBuilder() {
        final Builder builder = new Builder(scope, revision);
        builder.connections.addAll(connections);
        builder.triggerBindings.addAll(triggerBindings);
        builder.operationConfigurations.addAll(operationConfigurations);
        builder.environment.putAll(environment);
        builder.credentialBroker = credentialBroker;
        builder.schemaVersion = schemaVersion;
        return builder;
    }

    /**
     * Rejects duplicate identities, invalid schema, and missing broker selection.
     *
     * @throws IllegalArgumentException if the document is invalid
     */
    public void validate() {
        scope.prefix();
        if (!SCHEMA_VERSION.equals(schemaVersion) && !PREVIOUS_SCHEMA_VERSION.equals(schemaVersion)) {
            throw new IllegalArgumentException("unsupported configuration schema");
        }
        if (revision < 1) {
            throw new IllegalArgumentException("configuration revision is required");
        }
        if (connections.size() > 1000 || triggerBindings.size() > 10000 || operationConfigurations.size() > 10000) {
            throw new IllegalArgumentException("configuration document exceeds binding limits");
        }
        final Set<List<String>> identities = new HashSet<>();
        boolean hasSharedConnection = false;
        for (ConnectionConfiguration entry : connections) {
            if (!identities.add(Arrays.asList(entry.getConnectorId(), entry.getConnectionName()))) {
                throw new IllegalArgumentException("duplicate project connection");
            }
            if (!ProjectConfigurationRules.CONNECTOR_ID.matcher(entry.getConnectorId()).matches()
                    || entry.getConnectionName().isEmpty() || entry.getModulePath().isEmpty()
                    || entry.getProvider().isEmpty()) {
                throw new IllegalArgumentException("invalid project connection");
            }
            hasSharedConnection = hasSharedConnection || !entry.getSharedConnectionId().isEmpty();
        }
        final Set<List<String>> triggerIdentities = new HashSet<>();
        for (ObjectNode binding : triggerBindings) {
            final List<String> identity = Arrays.asList(ProjectConfigurationRules.text(binding, "connectorId"),
                    ProjectConfigurationRules.text(binding, "connectionName"),
                    ProjectConfigurationRules.text(binding, "triggerName"),
                    ProjectConfigurationRules.text(binding, "bindingName"));
            if (!identities.contains(identity.subList(0, 2)) || identity.get(2).isEmpty() || identity.get(3).isEmpty()
                    || binding.get("configuration") == null || !binding.get("configuration").isObject()
                    || !triggerIdentities.add(identity)) {
                throw new IllegalArgumentException("configuration trigger is invalid or duplicated");
            }
        }
        final Set<List<String>> operationIdentities = new HashSet<>();
        for (ObjectNode operation : operationConfigurations) {
            final List<String> identity = Arrays.asList(ProjectConfigurationRules.text(operation, "connectorId"),
                    ProjectConfigurationRules.text(operation, "connectionName"),
                    ProjectConfigurationRules.text(operation, "operationId"),
                    ProjectConfigurationRules.text(operation, "flowType"),
                    ProjectConfigurationRules.text(operation, "stepType"));
            if (!identities.contains(identity.subList(0, 2)) || identity.get(2).isEmpty()
                    || identity.get(3).isEmpty() || identity.get(4).isEmpty()
                    || operation.get("configuration") == null || !operation.get("configuration").isObject()
                    || !operationIdentities.add(identity)) {
                throw new IllegalArgumentException("configuration operation is invalid or duplicated");
            }
        }
        if (hasSharedConnection && credentialBroker == null) {
            throw new IllegalArgumentException("shared connection requires a credential broker");
        }
        if (PREVIOUS_SCHEMA_VERSION.equals(schemaVersion) && credentialBroker != null) {
            throw new IllegalArgumentException("previous configuration schema cannot declare a broker");
        }
        for (ConnectionConfiguration entry : connections) {
            if (!entry.getSharedConnectionId().isEmpty() && !SHARED_ID.matcher(entry.getSharedConnectionId()).matches()) {
                throw new IllegalArgumentException("configuration shared connection ID is invalid");
            }
        }
        if (environment.size() > 128) {
            throw new IllegalArgumentException("application environment exceeds field limit");
        }
        for (Map.Entry<String, EnvironmentValue> entry : environment.entrySet()) {
            if (!ProjectConfigurationRules.isApplicationEnvironmentName(entry.getKey())) {
                throw new IllegalArgumentException("invalid application environment name");
            }
            if (entry.getValue().getValue() != null) {
                ProjectConfigurationRules.validateEnvironmentValue(entry.getValue().getValue());
            } else {
                ProjectConfigurationRules.validateSecretReference(entry.getValue().getSecretRef(), scope, entry.getKey());
            }
        }
        if (credentialBroker != null) {
            ProjectConfigurationRules.validateBrokerEndpoint(credentialBroker.getEndpoint());
            ProjectConfigurationRules.validateSecretReference(credentialBroker.getTokenSecretRef(), scope,
                    ProjectConfigurationRules.CREDENTIAL_BROKER_SECRET_NAME);
        }
    }

    /**
     * Encodes the shared project configuration schema after validation.
     *
     * @return the JSON object in Go field order
     */
    public ObjectNode toJson() {
        validate();
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("schemaVersion", schemaVersion);
        result.set("scope", scope.toJson());
        result.put("revision", revision);
        final ArrayNode connectionArray = result.putArray("connections");
        for (ConnectionConfiguration entry : connections) {
            connectionArray.add(entry.toJson());
        }
        final ArrayNode triggerArray = result.putArray("triggerBindings");
        for (ObjectNode binding : triggerBindings) {
            triggerArray.add(binding.deepCopy());
        }
        final ArrayNode operationArray = result.putArray("operationConfigurations");
        for (ObjectNode operation : operationConfigurations) {
            operationArray.add(operation.deepCopy());
        }
        if (!environment.isEmpty()) {
            final ObjectNode environmentObject = result.putObject("environment");
            for (Map.Entry<String, EnvironmentValue> entry : new TreeMap<>(environment).entrySet()) {
                environmentObject.set(entry.getKey(), entry.getValue().toJson());
            }
        }
        if (credentialBroker != null) {
            result.set("credentialBroker", credentialBroker.toJson());
        }
        return result;
    }

    /**
     * Decodes and validates the shared document, including previous-schema documents.
     *
     * @param value the JSON value
     * @return the configuration
     * @throws IllegalArgumentException if the document is invalid
     */
    public static Configuration fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "configuration");
        ProjectConfigurationRules.requireFields(record, "configuration", "schemaVersion", "scope", "revision",
                "connections", "triggerBindings", "operationConfigurations", "environment", "credentialBroker");
        final JsonNode connectionArray = record.get("connections");
        final JsonNode triggerArray = record.get("triggerBindings");
        final JsonNode operationArray = record.get("operationConfigurations");
        if (connectionArray == null || !connectionArray.isArray() || triggerArray == null || !triggerArray.isArray()
                || operationArray == null || !operationArray.isArray()) {
            throw new IllegalArgumentException("configuration arrays are required");
        }
        final JsonNode revision = record.get("revision");
        if (revision != null && !revision.isNull() && !revision.isIntegralNumber()) {
            throw new IllegalArgumentException("configuration revision is invalid");
        }
        final Builder builder = new Builder(Scope.fromJson(record.get("scope")),
                revision == null || revision.isNull() ? 0 : revision.longValue());
        builder.schemaVersion = ProjectConfigurationRules.text(record, "schemaVersion");
        for (JsonNode entry : connectionArray) {
            builder.addConnection(ConnectionConfiguration.fromJson(entry));
        }
        for (JsonNode entry : triggerArray) {
            builder.addTriggerBinding(GoCompatibleJson.requireObject(entry, "trigger"));
        }
        for (JsonNode entry : operationArray) {
            builder.addOperationConfiguration(GoCompatibleJson.requireObject(entry, "operation"));
        }
        final JsonNode environment = record.get("environment");
        if (environment != null) {
            final Iterator<Map.Entry<String, JsonNode>> fields =
                    GoCompatibleJson.requireObject(environment, "environment").fields();
            while (fields.hasNext()) {
                final Map.Entry<String, JsonNode> field = fields.next();
                builder.putEnvironment(field.getKey(), EnvironmentValue.fromJson(field.getValue()));
            }
        }
        final JsonNode broker = record.get("credentialBroker");
        if (broker != null && !broker.isNull()) {
            builder.credentialBroker(CredentialBrokerConfiguration.fromJson(broker));
        }
        final Configuration result = builder.build();
        result.validate();
        return result;
    }

    /**
     * Returns the project scope.
     *
     * @return the scope
     */
    public Scope getScope() {
        return scope;
    }

    /**
     * Returns the document revision.
     *
     * @return the revision
     */
    public long getRevision() {
        return revision;
    }

    /**
     * Returns the connection entries.
     *
     * @return the unmodifiable entries
     */
    public List<ConnectionConfiguration> getConnections() {
        return connections;
    }

    /**
     * Returns copies of the Trigger binding objects.
     *
     * @return the bindings
     */
    public List<ObjectNode> getTriggerBindings() {
        return copyObjects(triggerBindings);
    }

    /**
     * Returns copies of the operation configuration objects.
     *
     * @return the operation configurations
     */
    public List<ObjectNode> getOperationConfigurations() {
        return copyObjects(operationConfigurations);
    }

    /**
     * Returns the application environment selections.
     *
     * @return the unmodifiable environment
     */
    public Map<String, EnvironmentValue> getEnvironment() {
        return environment;
    }

    /**
     * Returns the credential broker configuration.
     *
     * @return the broker, or {@code null}
     */
    public CredentialBrokerConfiguration getCredentialBroker() {
        return credentialBroker;
    }

    /**
     * Returns the document schema version.
     *
     * @return the schema version
     */
    public String getSchemaVersion() {
        return schemaVersion;
    }

    private static List<ObjectNode> copyObjects(final List<ObjectNode> objects) {
        final List<ObjectNode> copies = new ArrayList<>(objects.size());
        for (ObjectNode object : objects) {
            copies.add(object.deepCopy());
        }
        return Collections.unmodifiableList(copies);
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof Configuration)) {
            return false;
        }
        final Configuration that = (Configuration) other;
        return revision == that.revision && scope.equals(that.scope) && connections.equals(that.connections)
                && triggerBindings.equals(that.triggerBindings)
                && operationConfigurations.equals(that.operationConfigurations)
                && environment.equals(that.environment) && Objects.equals(credentialBroker, that.credentialBroker)
                && schemaVersion.equals(that.schemaVersion);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(scope, revision, connections, schemaVersion);
    }

    /**
     * Builds a {@link Configuration}.
     */
    public static final class Builder {
        private final Scope scope;
        private final long revision;
        private final List<ConnectionConfiguration> connections = new ArrayList<>();
        private final List<ObjectNode> triggerBindings = new ArrayList<>();
        private final List<ObjectNode> operationConfigurations = new ArrayList<>();
        private final Map<String, EnvironmentValue> environment = new LinkedHashMap<>();
        private CredentialBrokerConfiguration credentialBroker;
        private String schemaVersion = SCHEMA_VERSION;

        private Builder(final Scope scope, final long revision) {
            this.scope = scope;
            this.revision = revision;
        }

        /**
         * Adds a connection entry.
         *
         * @param value the entry
         * @return this builder
         */
        public Builder addConnection(final ConnectionConfiguration value) {
            connections.add(Objects.requireNonNull(value, "connection"));
            return this;
        }

        /**
         * Adds a Trigger binding object.
         *
         * @param value the binding
         * @return this builder
         */
        public Builder addTriggerBinding(final ObjectNode value) {
            triggerBindings.add(Objects.requireNonNull(value, "trigger binding").deepCopy());
            return this;
        }

        /**
         * Adds an operation configuration object.
         *
         * @param value the operation configuration
         * @return this builder
         */
        public Builder addOperationConfiguration(final ObjectNode value) {
            operationConfigurations.add(Objects.requireNonNull(value, "operation configuration").deepCopy());
            return this;
        }

        /**
         * Selects one application environment value.
         *
         * @param name the environment name
         * @param value the selection
         * @return this builder
         */
        public Builder putEnvironment(final String name, final EnvironmentValue value) {
            environment.put(name, Objects.requireNonNull(value, "environment value"));
            return this;
        }

        /**
         * Sets the credential broker.
         *
         * @param value the broker, or {@code null}
         * @return this builder
         */
        public Builder credentialBroker(final CredentialBrokerConfiguration value) {
            this.credentialBroker = value;
            return this;
        }

        /**
         * Sets the schema version.
         *
         * @param value the schema version
         * @return this builder
         */
        public Builder schemaVersion(final String value) {
            this.schemaVersion = value == null ? "" : value;
            return this;
        }

        /**
         * Builds the configuration without validating it.
         *
         * @return the configuration
         */
        public Configuration build() {
            return new Configuration(this);
        }
    }
}
