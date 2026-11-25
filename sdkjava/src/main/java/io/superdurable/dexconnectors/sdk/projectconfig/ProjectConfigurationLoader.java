// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;

/**
 * Loads a project from the hosted {@code DEX_PROJECT_*} contract.
 *
 * <p>Local Dex Web files are not supported in this release.
 *
 * <pre>{@code
 * LoadedProject project = ProjectConfigurationLoader.loadFromEnvironment(System.getenv(), s3Client);
 * RefreshingProjectCredentialProvider<SlackCredentials> provider = new RefreshingProjectCredentialProvider<>(
 *         project.getConnections(), new ConnectionKey("slack", "main"),
 *         SlackCredentials::fromJson, SlackCredentials::toJson);
 * }</pre>
 */
public final class ProjectConfigurationLoader {
    /** Environment variable that selects Dex Web's {@code connections.json}, unsupported in this release. */
    public static final String LOCAL_CONNECTIONS_FILE_ENVIRONMENT = "DEX_CONNECTOR_CONFIG_FILE";

    private static final String LOCAL_DEX_WEB_UNSUPPORTED = "Java SDK 0.1.0 supports hosted project configuration "
            + "(broker credentials, S3 object store supplied by the application) and static credentials only; "
            + "local Dex Web files are not supported yet";

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
     * Loads the trusted hosted {@code DEX_PROJECT_*} contract.
     *
     * <p>Hosted projects read versioned S3 through the application's client adapter;
     * {@code DEX_PROJECT_ALLOW_LOCAL_STORAGE=true} with a loopback or private
     * {@code DEX_PROJECT_STORAGE_ENDPOINT} allows an unencrypted local fixture store. Local Dex Web
     * files, selected by {@value #LOCAL_CONNECTIONS_FILE_ENVIRONMENT}, are not supported yet in
     * this release and fail fast.
     *
     * @param environment the environment variables, such as {@code System.getenv()}
     * @param s3Client the S3 client adapter for the hosted project
     * @return the loaded project
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if the selectors are invalid or conflict
     * @throws UnsupportedOperationException if local Dex Web files are selected
     */
    public static LoadedProject loadFromEnvironment(final Map<String, String> environment, final S3ObjectClient s3Client)
            throws IOException {
        final Map<String, String> values = environment == null ? Collections.<String, String>emptyMap() : environment;
        if (!value(values, LOCAL_CONNECTIONS_FILE_ENVIRONMENT).isEmpty()) {
            throw new UnsupportedOperationException(LOCAL_DEX_WEB_UNSUPPORTED);
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

    private static String value(final Map<String, String> values, final String name) {
        final String value = values.get(name);
        return value == null ? "" : value;
    }
}
