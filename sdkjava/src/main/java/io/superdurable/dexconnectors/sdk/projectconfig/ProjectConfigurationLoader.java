// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Map;
import java.util.Set;

/**
 * Loads a project from Dex Web's local files or the hosted {@code DEX_PROJECT_*} contract.
 *
 * <pre>{@code
 * LoadedProject project = ProjectConfigurationLoader.loadFromEnvironment(System.getenv(), null);
 * RefreshingProjectCredentialProvider<SlackCredentials> provider = new RefreshingProjectCredentialProvider<>(
 *         project.getConnections(), new ConnectionKey("slack", "main"),
 *         SlackCredentials::fromJson, SlackCredentials::toJson);
 * }</pre>
 */
public final class ProjectConfigurationLoader {
    /** Environment variable that selects Dex Web's {@code connections.json}. */
    public static final String LOCAL_CONNECTIONS_FILE_ENVIRONMENT = "DEX_CONNECTOR_CONFIG_FILE";

    private static final Set<String> LOCAL_CONNECTION_FIELDS = new HashSet<>(Arrays.asList("connectorId",
            "connectionName", "modulePath", "provider", "configuration", "authMethodId", "sharedConnectionId"));
    private static final String[] HOSTED_SELECTORS = {
        "DEX_PROJECT_ID", "DEX_PROJECT_SCOPE", "DEX_PROJECT_CONFIG_KEY", "DEX_PROJECT_STORAGE_BUCKET"};

    private ProjectConfigurationLoader() {
    }

    /**
     * Reads a pinned snapshot from a trusted object store without fetching credentials.
     *
     * @param objects the project object store
     * @param scope the project scope
     * @param reference the pinned snapshot
     * @param brokerTransport the broker HTTP transport, or {@code null} for the default
     * @return the loaded project
     * @throws IOException if storage fails
     */
    public static LoadedProject loadProjectFromObjectStore(
            final ObjectStore objects, final Scope scope, final SnapshotRef reference,
            final BrokerHttpTransport brokerTransport) throws IOException {
        return loadProjectFromObjectStore(objects, scope, reference, brokerTransport, null);
    }

    static LoadedProject loadProjectFromObjectStore(
            final ObjectStore objects, final Scope scope, final SnapshotRef reference,
            final BrokerHttpTransport brokerTransport, final ConnectionStore connectionStore) throws IOException {
        final ConfigurationStore configurations = new ConfigurationStore(objects, scope);
        final Configuration configuration = configurations.readSnapshot(reference);
        boolean hasSharedConnection = false;
        for (ConnectionConfiguration entry : configuration.getConnections()) {
            hasSharedConnection = hasSharedConnection || !entry.getSharedConnectionId().isEmpty();
        }
        if (connectionStore != null && hasSharedConnection) {
            throw new IllegalArgumentException("an externally supplied connection store cannot bind shared connections");
        }
        CredentialBrokerClient broker = null;
        final Map<ConnectionKey, SharedConnection> sharedConnections = new HashMap<>();
        if (configuration.getCredentialBroker() != null) {
            broker = new CredentialBrokerClient(configuration.getCredentialBroker(),
                    tokenReference -> configurations.readSecretValue(tokenReference,
                            ProjectConfigurationRules.CREDENTIAL_BROKER_SECRET_NAME),
                    () -> {
                        final Configuration current = configurations.readConfiguration().getConfiguration();
                        if (current.getCredentialBroker() == null) {
                            throw new IllegalArgumentException("credential broker token reference is absent");
                        }
                        return current.getCredentialBroker().getTokenSecretRef();
                    },
                    brokerTransport != null ? brokerTransport : BrokerHttpTransport.httpUrlConnection());
            for (ConnectionConfiguration entry : configuration.getConnections()) {
                if (!entry.getSharedConnectionId().isEmpty()) {
                    sharedConnections.put(new ConnectionKey(entry.getConnectorId(), entry.getConnectionName()),
                            new SharedConnection(entry.getSharedConnectionId(), broker));
                }
            }
        }
        final ConnectionStore connections = connectionStore != null
                ? connectionStore : new ConnectionStore(objects, scope, sharedConnections);
        return new LoadedProject(configuration, reference, connections, configurations, broker, sharedConnections);
    }

    /**
     * Loads Dex Web's owner-only {@code connections.json} and its {@code use-configurations.json}
     * sidecar.
     *
     * <p>Refreshed credentials are written back to {@code connections.json}; Trigger inboxes are
     * kept in a private directory beside it.
     *
     * @param path the {@code connections.json} path
     * @return the loaded project in the {@code local/preview} scope
     * @throws IOException if a file cannot be read
     * @throws IllegalArgumentException if a file is not owner-only or has an unsupported schema
     */
    public static LoadedProject loadDexWebProject(final Path path) throws IOException {
        final Path connectionsPath = path.toAbsolutePath();
        final ObjectNode source = DexWebFiles.readPrivateFile(connectionsPath);
        if (!Configuration.LOCAL_CONNECTIONS_SCHEMA_VERSION.equals(ProjectConfigurationRules.text(source, "schemaVersion"))) {
            throw new IllegalArgumentException("unsupported connector configuration schema version");
        }
        final Path usesPath = connectionsPath.resolveSibling("use-configurations.json");
        final ObjectNode uses = Files.exists(usesPath, LinkOption.NOFOLLOW_LINKS)
                ? DexWebFiles.readPrivateFile(usesPath) : GoCompatibleJson.newObject();
        if (uses.size() > 0 && !Configuration.LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION.equals(
                ProjectConfigurationRules.text(uses, "schemaVersion"))) {
            throw new IllegalArgumentException("unsupported connector use configuration schema version");
        }
        final JsonNode rawConnections = source.get("connections");
        if (rawConnections == null || !rawConnections.isArray()) {
            throw new IllegalArgumentException("connections.json requires connections");
        }
        final Scope scope = new Scope("local", "preview");
        final Configuration.Builder document = Configuration.newBuilder(scope, 1);
        int index = 0;
        for (JsonNode raw : rawConnections) {
            final ObjectNode record = GoCompatibleJson.requireObject(raw, "connection " + index++);
            final ObjectNode selected = GoCompatibleJson.newObject();
            final Iterator<Map.Entry<String, JsonNode>> fields = record.fields();
            while (fields.hasNext()) {
                final Map.Entry<String, JsonNode> field = fields.next();
                if (LOCAL_CONNECTION_FIELDS.contains(field.getKey())) {
                    selected.set(field.getKey(), field.getValue());
                }
            }
            final ConnectionConfiguration entry = ConnectionConfiguration.fromJson(selected);
            if (entry.getConnectorId().isEmpty() || entry.getConnectionName().isEmpty()
                    || entry.getModulePath().isEmpty() || entry.getProvider().isEmpty()) {
                throw new IllegalArgumentException("local connection identity is incomplete");
            }
            document.addConnection(entry);
        }
        addObjects(source.get("triggerBindings"), "trigger binding", document, true);
        addObjects(uses.get("operationConfigurations"), "operation configuration", document, false);
        final InMemoryObjectStore store = new InMemoryObjectStore();
        final ConfigurationStore configurations = new ConfigurationStore(store, scope);
        final ConfigurationStore.VersionedConfiguration written = configurations.writeConfiguration(0, document.build());
        final SnapshotRef reference = configurations.freezeConfiguration(written.getConfiguration().getRevision());
        final DexWebObjectStore localObjects = new DexWebObjectStore(connectionsPath, scope);
        localObjects.initialize();
        return loadProjectFromObjectStore(store, scope, reference, null, new ConnectionStore(localObjects, scope));
    }

    /**
     * Loads Dex Web local files or the trusted hosted {@code DEX_PROJECT_*} contract.
     *
     * <p>{@value #LOCAL_CONNECTIONS_FILE_ENVIRONMENT} selects Dex Web and must not be combined with
     * hosted selectors. Hosted projects read versioned S3 through the application's client
     * adapter; {@code DEX_PROJECT_ALLOW_LOCAL_STORAGE=true} with a loopback or private
     * {@code DEX_PROJECT_STORAGE_ENDPOINT} allows an unencrypted local fixture store.
     *
     * @param environment the environment variables, such as {@code System.getenv()}
     * @param s3Client the S3 client adapter for a hosted project, or {@code null} for Dex Web only
     * @return the loaded project
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if the selectors are invalid or conflict
     */
    public static LoadedProject loadFromEnvironment(final Map<String, String> environment, final S3ObjectClient s3Client)
            throws IOException {
        final Map<String, String> values = environment == null ? Collections.<String, String>emptyMap() : environment;
        final String localPath = value(values, LOCAL_CONNECTIONS_FILE_ENVIRONMENT);
        if (!localPath.isEmpty()) {
            for (String selector : HOSTED_SELECTORS) {
                if (!value(values, selector).isEmpty()) {
                    throw new IllegalArgumentException("local and hosted project configuration selectors conflict");
                }
            }
            return loadDexWebProject(Paths.get(localPath));
        }
        final Scope scope = new Scope(value(values, "DEX_PROJECT_ID"), value(values, "DEX_PROJECT_SCOPE"));
        scope.prefix();
        final String allowLocal = values.containsKey("DEX_PROJECT_ALLOW_LOCAL_STORAGE")
                ? values.get("DEX_PROJECT_ALLOW_LOCAL_STORAGE") : "false";
        if (!"true".equals(allowLocal) && !"false".equals(allowLocal)) {
            throw new IllegalArgumentException("project local storage opt-in must be true or false");
        }
        final String endpoint = value(values, "DEX_PROJECT_STORAGE_ENDPOINT");
        if (!endpoint.isEmpty()
                && (!"true".equals(allowLocal) || !ProjectConfigurationRules.isLocalStorageEndpoint(endpoint))) {
            throw new IllegalArgumentException("custom project storage endpoint requires explicit local opt-in");
        }
        if ("true".equals(allowLocal) && endpoint.isEmpty()) {
            throw new IllegalArgumentException("local project storage requires an endpoint");
        }
        final String bucket = value(values, "DEX_PROJECT_STORAGE_BUCKET");
        if (bucket.isEmpty()) {
            throw new IllegalArgumentException("project storage bucket is required");
        }
        if (s3Client == null) {
            throw new IllegalArgumentException("a hosted project requires an S3 object client");
        }
        final S3ObjectStore objects = S3ObjectStore.create(s3Client, bucket, value(values, "DEX_PROJECT_STORAGE_PREFIX"),
                value(values, "DEX_PROJECT_STORAGE_KMS_KEY_ARN"), "true".equals(allowLocal));
        final SnapshotRef reference = new SnapshotRef(value(values, "DEX_PROJECT_CONFIG_KEY"),
                value(values, "DEX_PROJECT_CONFIG_VERSION"), value(values, "DEX_PROJECT_CONFIG_DIGEST"));
        return loadProjectFromObjectStore(objects, scope, reference, null);
    }

    private static void addObjects(
            final JsonNode array, final String label, final Configuration.Builder document,
            final boolean isTriggerBinding) {
        if (array == null || array.isNull()) {
            return;
        }
        if (!array.isArray()) {
            throw new IllegalArgumentException(label + " list must be an array");
        }
        for (JsonNode item : array) {
            final ObjectNode object = GoCompatibleJson.requireObject(item, label);
            if (isTriggerBinding) {
                document.addTriggerBinding(object);
            } else {
                document.addOperationConfiguration(object);
            }
        }
    }

    private static String value(final Map<String, String> values, final String name) {
        final String value = values.get(name);
        return value == null ? "" : value;
    }
}
