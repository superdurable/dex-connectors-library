// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hackernews.StoryFeed.*;
import io.superdurable.dexconnectors.sdk.*;
import java.nio.file.Files;
import java.util.Collections;

/** Serves the story feed Flow with the static hacker-news-public connection. */
public final class StoryFeedWorker {
    private StoryFeedWorker() {}

    /** Serve the feed Flow, or pass --read to run it once and print the returned snapshot. */
    public static void main(String[] args) throws Exception {
        FeedFlow flow = new FeedFlow(new ListStoryIDsOperation());
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        if (args.length == 1 && "--read".equals(args[0])) {
            StoryIDs output = readOnce(flow, address);
            System.out.println(new com.fasterxml.jackson.databind.ObjectMapper().writeValueAsString(output));
            return;
        }
        serve(flow, address);
    }

    static void serve(Flow<?> flow, String address) throws Exception {
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache = BlobCache.open(new BlobCacheConfig(Files.createTempDirectory("story-feed-blobs").toString(), 16L << 20));
             Worker worker = new Worker(registry, cache, WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    static StoryIDs readOnce(FeedFlow flow, String address) throws Exception {
        return readOnce(flow, address, new ListStoryIDsInput("top", 2), StoryIDs.class);
    }

    static <I, O> O readOnce(Flow<I> flow, String address, I input, Class<O> outputType) throws Exception {
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache = BlobCache.open(new BlobCacheConfig(Files.createTempDirectory("story-feed-blobs").toString(), 16L << 20));
             Client client = new Client(registry, cache, new ClientOptions(address))) {
            java.util.concurrent.FutureTask<Void> serving;
            O output;
            try (Worker worker = new Worker(registry, cache, WorkerOptions.newBuilder().serverAddress(address).build())) {
                serving = new java.util.concurrent.FutureTask<>(() -> { worker.start(); return null; });
                new Thread(serving, "story-feed-worker").start();
                worker.waitPullRegistered(java.time.Duration.ofSeconds(30));
                String flowId = flow.getFlowType() + "-" + java.util.UUID.randomUUID();
                client.startFlow(flow, flowId, input);
                FlowResult result = client.waitForFlow(flowId, java.time.Duration.ofSeconds(30));
                if (result.getStatus() != FlowStatus.COMPLETED) throw new IllegalStateException(flow.getFlowType() + " did not complete");
                output = result.getSingleOutput(outputType);
            }
            serving.get(30, java.util.concurrent.TimeUnit.SECONDS);
            return output;
        }
    }

    /** One provider query followed by completion with its story feed. */
    public static final class FeedFlow implements Flow<ListStoryIDsInput> {
        private final QueryStep<ListStoryIDsInput, ListStoryIDsInput, StoryIDs, StoryFeedResult> query;
        private final ReturnStoryIDs complete = new ReturnStoryIDs();

        /** Bind the operation during Worker registration. */
        public FeedFlow(ListStoryIDsOperation operation) {
            query = new QueryStep<ListStoryIDsInput, ListStoryIDsInput, StoryIDs, StoryFeedResult>(
                    StoryFeed.newListStoryIDsStep("ListStoryIDs", ListStoryIDsInput.class,
                            new StepAnnotations("story-feed", "Story feed", "Read public Hacker News story IDs."),
                            operation, new ConnectionRef("hacker-news", "hacker-news-public"), input -> input)
                            .branch("listed", ReturnStoryIDs.class).build()) {};
        }

        @Override public String getFlowType() { return "JavaHackerNewsStoryFeed"; }
        @Override public StepList<ListStoryIDsInput> getSteps() { return StepList.startStep(query).otherSteps(complete); }
        @Override public PersistenceSchema getPersistenceSchema() { return PersistenceSchema.of(); }
    }

    // dex:group group-id:story-feed group-label:"Story feed"
    // dex:explanation text:"Complete with the provider's visible story IDs."
    public static final class ReturnStoryIDs implements Step<StoryFeedResult> {
        @Override public Class<StoryFeedResult> getInputType() { return StoryFeedResult.class; }
        @Override public StepDecision execute(Context context, StoryFeedResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
