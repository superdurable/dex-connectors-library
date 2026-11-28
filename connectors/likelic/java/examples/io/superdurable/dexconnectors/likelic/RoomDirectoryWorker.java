// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.likelic;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.likelic.RoomDirectory.*;
import io.superdurable.dexconnectors.likelic.RoomDirectory.Credentials;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the room lookup Flow with the static likelic connection. */
public final class RoomDirectoryWorker {
    private RoomDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("LIKELIC_API_KEY"), "LIKELIC_API_KEY is required"));
        Credentials credentials =
                new Credentials(
                        java.util.Objects.requireNonNull(
                                System.getenv("LIKELIC_CUSTOMER_CODE"),
                                "LIKELIC_CUSTOMER_CODE is required"),
                        token);
        RoomDirectoryFlow flow = new RoomDirectoryFlow(new ListRoomsOperation(call -> credentials));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("room-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its room catalog. */
    public static final class RoomDirectoryFlow implements Flow<ListRoomsInput> {
        private final QueryStep<ListRoomsInput, ListRoomsInput, ListRoomsOutput, ListRoomsResult>
                query;
        private final ReturnRooms complete = new ReturnRooms();

        /** Bind the operation during Worker registration. */
        public RoomDirectoryFlow(ListRoomsOperation operation) {
            query =
                    new QueryStep<ListRoomsInput, ListRoomsInput, ListRoomsOutput, ListRoomsResult>(
                            RoomDirectory.newListRoomsStep(
                                            "ListRooms",
                                            ListRoomsInput.class,
                                            new StepAnnotations(
                                                    "room-directory",
                                                    "Room catalog",
                                                    "Read the available room catalog."),
                                            operation,
                                            new ConnectionRef("likelic", "likelic"),
                                            input -> input)
                                    .branch("found", ReturnRooms.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaLikelicRoomDirectory";
        }

        @Override
        public StepList<ListRoomsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:room-directory group-label:"Room catalog"
    // dex:explanation text:"Complete with the provider's visible room catalog."
    public static final class ReturnRooms implements Step<ListRoomsResult> {
        @Override
        public Class<ListRoomsResult> getInputType() {
            return ListRoomsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListRoomsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
