// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;

import java.io.IOException;
import java.nio.file.Files;
import java.util.Arrays;
import java.util.Objects;

/** Serve the single-person and directory Flows on the personio connection. */
public final class PersonDirectoryWorker {
    private PersonDirectoryWorker() {}

    /**
     * Start a Worker using an in-memory project seeded from the integration credentials in its
     * environment.
     */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        LoadedProject project =
                exampleProject(
                        Objects.requireNonNull(
                                System.getenv("PERSONIO_CLIENT_ID"),
                                "PERSONIO_CLIENT_ID is required"),
                        Objects.requireNonNull(
                                System.getenv("PERSONIO_CLIENT_SECRET"),
                                "PERSONIO_CLIENT_SECRET is required"));
        PersonioClient client = PersonioClient.fromProject(project, "personio");
        Registry registry =
                new Registry(
                        Arrays.<Flow<?>>asList(
                                new GetPersonFlow(client.getPerson()),
                                new ListPersonsFlow(client.listPersons())));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("personio-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    static LoadedProject exampleProject(String id, String secret) throws IOException {
        if (id.isEmpty() || secret.isEmpty())
            throw new IllegalArgumentException("Personio integration credentials are required");
        InMemoryObjectStore objects = new InMemoryObjectStore();
        Scope scope = new Scope("personio-directory", "preview");
        ObjectMapper json = new ObjectMapper();
        ObjectNode credentials =
                json.createObjectNode().put("client_id", id).put("client_secret", secret);
        ConnectionStore connections = new ConnectionStore(objects, scope);
        connections.replaceCredential(
                new ConnectionKey("personio", "personio"),
                0,
                new CredentialMaterial(credentials, null, "apiKey"));
        ConfigurationStore configs = new ConfigurationStore(objects, scope);
        ConnectionConfiguration entry =
                new ConnectionConfiguration(
                        "personio",
                        "personio",
                        "github.com/superdurable/dex-connectors-library/connectors/personio/go",
                        "personio",
                        json.createObjectNode());
        Configuration written =
                configs.writeConfiguration(
                                0, Configuration.newBuilder(scope, 1).addConnection(entry).build())
                        .getConfiguration();
        return ProjectConfigurationLoader.loadProjectFromObjectStore(
                objects, scope, configs.freezeConfiguration(written.getRevision()), null);
    }
}
