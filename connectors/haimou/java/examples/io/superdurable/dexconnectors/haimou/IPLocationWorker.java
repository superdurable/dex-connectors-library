// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.haimou;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.haimou.IPLocation.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the IP lookup Flow with the static haimou connection. */
public final class IPLocationWorker {
    private IPLocationWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("HAIMOU_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("HAIMOU_API_KEY is required");
        LocationFlow flow =
                new LocationFlow(new LookupIPLocationOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("haimou-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its IPv4 location. */
    public static final class LocationFlow implements Flow<LookupIPLocationInput> {
        private final LookupIPLocationOperation operation;

        /** Bind the operation during Worker registration. */
        public LocationFlow(LookupIPLocationOperation operation) {
            this.operation = operation;
        }

        @Override
        public String getFlowType() {
            return "JavaHaimouIPLocation";
        }

        @Override
        public StepList<LookupIPLocationInput> getSteps() {
            return StepList.startStep(new LookupIPLocation(operation))
                    .otherSteps(new ReturnLocation());
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:ip-location group-label:"IP location"
    // dex:explanation text:"Look up the location of the supplied IPv4 address."
    public static final class LookupIPLocation
            extends QueryStep<
                    LookupIPLocationInput,
                    LookupIPLocationInput,
                    LookupIPLocationOutput,
                    LookupIPLocationResult> {
        LookupIPLocation(LookupIPLocationOperation operation) {
            super(
                    IPLocation.newLookupIPLocationStep(
                                    "LookupIPLocation",
                                    LookupIPLocationInput.class,
                                    new StepAnnotations(
                                            "ip-location",
                                            "IP location",
                                            "Look up the supplied IPv4 address."),
                                    operation,
                                    new ConnectionRef("haimou", "haimou"),
                                    input -> input)
                            .branch("found", ReturnLocation.class)
                            .build());
        }
    }

    // dex:group group-id:ip-location group-label:"IP location"
    // dex:explanation text:"Complete with the country, city and network returned by HAIMOU."
    public static final class ReturnLocation implements Step<LookupIPLocationResult> {
        @Override
        public Class<LookupIPLocationResult> getInputType() {
            return LookupIPLocationResult.class;
        }

        @Override
        public StepDecision execute(Context context, LookupIPLocationResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
