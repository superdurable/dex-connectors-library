// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dexconnectors.hackernews.ArticleThreads.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;

@Tag("dex-dev")
class ArticleThreadFlowTest {
    @Test void readArticleAfterTemporaryFailure() throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String origin = "https://hn.algolia.com/api/v1";
        AtomicInteger calls = new AtomicInteger();
        GetArticleThreadOperation operation = new GetArticleThreadOperation(origin, request -> {
            assertEquals(origin + "/items/42", request.getUrl());
            return response(calls.incrementAndGet() == 1 ? 503 : 200, "{\"id\":42,\"title\":\"Article\",\"children\":[{\"id\":7,\"children\":[{\"id\":9}]}]}");
        }, 1024, Clock.systemUTC());
        ArticleThread result = StoryFeedWorker.readOnce(new ArticleThreadWorker.ArticleThreadFlow(operation), address, new GetArticleThreadInput(42), ArticleThread.class);
        assertEquals("https://news.ycombinator.com/item?id=9", result.comments.get(0).replies.get(0).discussionUrl);
        assertEquals(2, calls.get());
    }

    private static ProviderHttpResponse response(int status, String body) {
        return ProviderHttpResponse.buffered(status, ProviderHttpHeaders.EMPTY, body.getBytes(StandardCharsets.UTF_8));
    }
}
