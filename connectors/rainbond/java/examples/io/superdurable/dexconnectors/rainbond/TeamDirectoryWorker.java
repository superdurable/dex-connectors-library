// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rainbond;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.rainbond.TeamDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the team lookup Flow with the static rainbond connection. */
public final class TeamDirectoryWorker {
    private TeamDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("RAINBOND_API_KEY"), "RAINBOND_API_KEY is required"));
        String origin =
                java.util.Objects.requireNonNull(
                        System.getenv("RAINBOND_BASE_URL"), "RAINBOND_BASE_URL is required");
        TeamDirectoryFlow flow =
                new TeamDirectoryFlow(new ListTeamsOperation(call -> token, origin));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("team-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its team catalog. */
    public static final class TeamDirectoryFlow implements Flow<ListTeamsInput> {
        private final QueryStep<ListTeamsInput, ListTeamsInput, ListTeamsOutput, ListTeamsResult>
                query;
        private final ReturnTeams complete = new ReturnTeams();

        /** Bind the operation during Worker registration. */
        public TeamDirectoryFlow(ListTeamsOperation operation) {
            query =
                    new QueryStep<ListTeamsInput, ListTeamsInput, ListTeamsOutput, ListTeamsResult>(
                            TeamDirectory.newListTeamsStep(
                                            "ListTeams",
                                            ListTeamsInput.class,
                                            new StepAnnotations(
                                                    "team-directory",
                                                    "Team catalog",
                                                    "Read the available team catalog."),
                                            operation,
                                            new ConnectionRef("rainbond", "rainbond"),
                                            input -> input)
                                    .branch("listed", ReturnTeams.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaRainbondTeamDirectory";
        }

        @Override
        public StepList<ListTeamsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:team-directory group-label:"Team catalog"
    // dex:explanation text:"Complete with the provider's visible team catalog."
    public static final class ReturnTeams implements Step<ListTeamsResult> {
        @Override
        public Class<ListTeamsResult> getInputType() {
            return ListTeamsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListTeamsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
