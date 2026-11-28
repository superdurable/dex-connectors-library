// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.unisound;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.unisound.VoiceDirectory.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the voice lookup Flow with the static unisound connection. */
public final class VoiceDirectoryWorker {
    private VoiceDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("UNISOUND_API_KEY"), "UNISOUND_API_KEY is required"));
        VoiceDirectoryFlow flow =
                new VoiceDirectoryFlow(new ListSystemVoicesOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("voice-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its voice catalog. */
    public static final class VoiceDirectoryFlow implements Flow<ListSystemVoicesInput> {
        private final QueryStep<
                        ListSystemVoicesInput,
                        ListSystemVoicesInput,
                        ListSystemVoicesOutput,
                        ListSystemVoicesResult>
                query;
        private final ReturnVoices complete = new ReturnVoices();

        /** Bind the operation during Worker registration. */
        public VoiceDirectoryFlow(ListSystemVoicesOperation operation) {
            query =
                    new QueryStep<
                            ListSystemVoicesInput,
                            ListSystemVoicesInput,
                            ListSystemVoicesOutput,
                            ListSystemVoicesResult>(
                            VoiceDirectory.newListSystemVoicesStep(
                                            "ListSystemVoices",
                                            ListSystemVoicesInput.class,
                                            new StepAnnotations(
                                                    "voice-directory",
                                                    "Voice catalog",
                                                    "Read the available voice catalog."),
                                            operation,
                                            new ConnectionRef("unisound", "unisound"),
                                            input -> input)
                                    .branch("found", ReturnVoices.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaUnisoundVoiceDirectory";
        }

        @Override
        public StepList<ListSystemVoicesInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:voice-directory group-label:"Voice catalog"
    // dex:explanation text:"Complete with the provider's visible voice catalog."
    public static final class ReturnVoices implements Step<ListSystemVoicesResult> {
        @Override
        public Class<ListSystemVoicesResult> getInputType() {
            return ListSystemVoicesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListSystemVoicesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
