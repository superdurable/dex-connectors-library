// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.nio.file.Files;
import java.util.Arrays;
import java.util.Objects;

/** Serve the single-worker and directory Flows on the rippling connection. */
public final class WorkerDirectoryWorker {
    private WorkerDirectoryWorker() {}

    /**
     * Start a Worker using a Rippling API token supplied by its environment. Hosted applications
     * use RipplingClient.fromProject.
     */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String baseUrl =
                System.getenv().getOrDefault("RIPPLING_BASE_URL", "https://rest.ripplingapis.com");
        SecretString token =
                new SecretString(
                        Objects.requireNonNull(
                                System.getenv("RIPPLING_API_TOKEN"),
                                "RIPPLING_API_TOKEN is required"));
        RipplingClient client =
                new RipplingClient(
                        call -> token,
                        baseUrl,
                        new ProviderHttpClient(
                                java.time.Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                        1 << 20);
        Registry registry =
                new Registry(
                        Arrays.<Flow<?>>asList(
                                new GetWorkerFlow(client.getWorker()),
                                new ListWorkersFlow(client.listWorkers())));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("rippling-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }
}
