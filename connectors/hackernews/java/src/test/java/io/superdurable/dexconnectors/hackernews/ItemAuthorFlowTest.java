// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dexconnectors.hackernews.PublicResources.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;

@Tag("dex-dev")
class ItemAuthorFlowTest {
    @Test void resolveAuthorAfterTemporaryFailure() throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String origin = "https://hacker-news.firebaseio.com/v0";
        AtomicInteger itemCalls = new AtomicInteger();
        AtomicInteger profileCalls = new AtomicInteger();
        GetItemOperation item = new GetItemOperation(origin, request -> {
            assertEquals(origin + "/item/42.json", request.getUrl());
            itemCalls.incrementAndGet();
            return response(200, "{\"id\":42,\"type\":\"story\",\"by\":\"author\"}");
        }, 1024, Clock.systemUTC());
        GetUserOperation author = new GetUserOperation(origin, request -> {
            assertEquals(origin + "/user/author.json", request.getUrl());
            return response(profileCalls.incrementAndGet() == 1 ? 503 : 200, "{\"id\":\"author\",\"karma\":12}");
        }, 1024, Clock.systemUTC());
        User result = StoryFeedWorker.readOnce(new ItemAuthorWorker.ItemAuthorFlow(item, author), address, new GetItemInput(42), User.class);
        assertEquals("https://news.ycombinator.com/user?id=author", result.profileUrl);
        assertEquals(12, result.karma);
        assertEquals(1, itemCalls.get());
        assertEquals(2, profileCalls.get());
    }

    private static ProviderHttpResponse response(int status, String body) {
        return ProviderHttpResponse.buffered(status, ProviderHttpHeaders.EMPTY, body.getBytes(StandardCharsets.UTF_8));
    }
}
