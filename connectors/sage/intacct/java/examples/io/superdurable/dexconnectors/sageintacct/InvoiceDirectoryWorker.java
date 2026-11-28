// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.nio.file.Files;
import java.util.Arrays;
import java.util.Objects;

/** Serve the invoice lookup and listing Flows on the sage-intacct connection. */
public final class InvoiceDirectoryWorker {
    private InvoiceDirectoryWorker() {}

    /**
     * Start a Worker using a tenant access token supplied by its environment. Hosted applications
     * use SageIntacctClient.fromProject for renewal.
     */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String baseUrl =
                System.getenv().getOrDefault("SAGE_INTACCT_BASE_URL", "https://api.intacct.com");
        String token =
                Objects.requireNonNull(
                        System.getenv("SAGE_INTACCT_ACCESS_TOKEN"),
                        "SAGE_INTACCT_ACCESS_TOKEN is required");
        SageIntacctCredentials credentials =
                new SageIntacctCredentials(
                        "", new SecretString(""), new SecretString(token), new SecretString(""));
        SageIntacctClient client =
                new SageIntacctClient(
                        call -> credentials,
                        baseUrl,
                        new ProviderHttpClient(
                                java.time.Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                        1 << 20);
        Registry registry =
                new Registry(
                        Arrays.<Flow<?>>asList(
                                new GetInvoiceFlow(client.getInvoice()),
                                new ListInvoicesFlow(client.listInvoices())));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("sage-intacct-blobs").toString(),
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
