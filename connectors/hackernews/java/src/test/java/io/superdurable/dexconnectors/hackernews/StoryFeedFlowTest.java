// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dexconnectors.hackernews.StoryFeed.ListStoryIDsOperation;
import io.superdurable.dexconnectors.hackernews.StoryFeed.StoryIDs;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

@Tag("dex-dev")
class StoryFeedFlowTest {
    @Test void retryThenComplete() throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListStoryIDsOperation operation = new ListStoryIDsOperation("https://hacker-news.firebaseio.com/v0", request ->
                ProviderHttpResponse.buffered(calls.incrementAndGet() == 1 ? 503 : 200, ProviderHttpHeaders.EMPTY,
                        "[101,102,103]".getBytes(StandardCharsets.UTF_8)), 1 << 20, java.time.Clock.systemUTC());
        StoryIDs result = StoryFeedWorker.readOnce(new StoryFeedWorker.FeedFlow(operation), address);
        assertEquals(java.util.Arrays.asList(101L, 102L), result.ids);
        assertTrue(result.hasMore);
        assertEquals(2, calls.get());
    }
}
