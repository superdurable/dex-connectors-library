// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dexconnectors.hackernews.SearchQueries.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class SearchFlowTest {
    private static final String ORIGIN = "https://hn.algolia.com/api/v1";
    private static final String ADDRESS =
            System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");

    @Test
    void retryStorySearch() throws Exception {
        AtomicInteger requests = new AtomicInteger();
        SearchStoriesOperation operation =
                new SearchStoriesOperation(
                        ORIGIN, provider(requests, "&tags=story"), 4096, Clock.systemUTC());
        StorySearchPage page =
                StoryFeedWorker.readOnce(
                        new StorySearchWorker.StorySearchFlow(operation),
                        ADDRESS,
                        new SearchStoriesInput("compiler", "", null, null, null, 0, 0, 2),
                        StorySearchPage.class);
        assertEquals(42L, page.stories.get(0).id);
        assertEquals(1L, page.nextPage);
        assertEquals(2, requests.get());
    }

    @Test
    void retryItemSearch() throws Exception {
        AtomicInteger requests = new AtomicInteger();
        SearchItemsOperation operation =
                new SearchItemsOperation(ORIGIN, provider(requests, ""), 4096, Clock.systemUTC());
        ItemSearchPage page =
                StoryFeedWorker.readOnce(
                        new ItemSearchWorker.ItemSearchFlow(operation),
                        ADDRESS,
                        new SearchItemsInput("compiler", "", "", null, 0, 2),
                        ItemSearchPage.class);
        assertEquals(42L, page.items.get(0).id);
        assertEquals(1L, page.nextPage);
        assertEquals(2, requests.get());
    }

    private ProviderHttpTransport provider(AtomicInteger requests, String suffix) {
        return request -> {
            assertEquals(
                    ORIGIN + "/search?hitsPerPage=2&page=0&query=compiler" + suffix,
                    request.getUrl());
            byte[] body =
                    "{\"page\":0,\"nbPages\":2,\"nbHits\":3,\"hits\":[{\"objectID\":\"42\",\"created_at_i\":1,\"_tags\":[\"story\"],\"title\":\"Release\"}]}"
                            .getBytes(StandardCharsets.UTF_8);
            return ProviderHttpResponse.buffered(
                    requests.incrementAndGet() == 1 ? 503 : 200, ProviderHttpHeaders.EMPTY, body);
        };
    }
}
