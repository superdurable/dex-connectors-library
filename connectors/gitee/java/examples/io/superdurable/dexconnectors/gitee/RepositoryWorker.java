// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;

import java.io.IOException;
import java.nio.file.Files;
import java.util.Objects;

/** Serve the repository lookup Flow on the gitee connection. */
public final class RepositoryWorker {
    private RepositoryWorker() {}

    /**
     * Start a Worker using an in-memory project seeded from the integration credentials in its
     * environment.
     */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String token =
                Objects.requireNonNull(
                        System.getenv("GITEE_ACCESS_TOKEN"), "GITEE_ACCESS_TOKEN is required");
        ObjectNode settings = new ObjectMapper().createObjectNode();
        LoadedProject project = exampleProject(token, settings);
        Registry registry =
                new Registry(
                        java.util.Collections.<Flow<?>>singletonList(
                                new GetRepositoryFlow(
                                        GetRepositoryOperation.fromProject(project, "gitee"))));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("gitee-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    static LoadedProject exampleProject(String token, ObjectNode settings) throws IOException {
        if (token.isEmpty())
            throw new IllegalArgumentException("Repository access token is required");
        InMemoryObjectStore objects = new InMemoryObjectStore();
        Scope scope = new Scope("gitee-directory", "preview");
        ObjectMapper json = new ObjectMapper();
        ObjectNode credentials = json.createObjectNode().put("access_token", token);
        ConnectionStore connections = new ConnectionStore(objects, scope);
        connections.replaceCredential(
                new ConnectionKey("gitee", "gitee"),
                0,
                new CredentialMaterial(credentials, null, "apiKey"));
        ConfigurationStore configs = new ConfigurationStore(objects, scope);
        ConnectionConfiguration entry =
                new ConnectionConfiguration(
                        "gitee",
                        "gitee",
                        "github.com/superdurable/dex-connectors-library/connectors/gitee/go",
                        "gitee",
                        settings);
        Configuration written =
                configs.writeConfiguration(
                                0, Configuration.newBuilder(scope, 1).addConnection(entry).build())
                        .getConfiguration();
        return ProjectConfigurationLoader.loadProjectFromObjectStore(
                objects, scope, configs.freezeConfiguration(written.getRevision()), null);
    }
}
