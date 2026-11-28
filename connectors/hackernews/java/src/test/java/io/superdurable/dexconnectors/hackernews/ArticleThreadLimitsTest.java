// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dexconnectors.hackernews.ArticleThreads.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.util.StringJoiner;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;

class ArticleThreadLimitsTest {
    @Test void boundsDepthAndTotalCommentsAcrossTheTree() throws Exception {
        assertTreeBranch(63, 1, "found");
        assertTreeBranch(64, 1, "invalidResponse");
        assertTreeBranch(2, 4999, "found");
        assertTreeBranch(2, 5000, "invalidResponse");
    }

    private void assertTreeBranch(int depth, int width, String expected) throws Exception {
        StringJoiner leaves = new StringJoiner(",");
        for (int index = 0; index < width; index++) leaves.add("{\"id\":" + (index + 2) + ",\"children\":[]}");
        String children = leaves.toString();
        for (int level = 1; level < depth; level++) children = "{\"id\":" + (10000 + level) + ",\"children\":[" + children + "]}";
        byte[] body = ("{\"id\":1,\"title\":\"Article\",\"children\":[" + children + "]}").getBytes(StandardCharsets.UTF_8);
        GetArticleThreadOperation operation = new GetArticleThreadOperation("https://hn.algolia.com/api/v1", request ->
                ProviderHttpResponse.buffered(200, ProviderHttpHeaders.EMPTY, body), 1 << 20, Clock.systemUTC());
        Call call = Call.create(CallId.of(java.util.UUID.randomUUID().toString()), new ConnectionRef("hacker-news", "public"),
                new OperationRef("hacker-news-daily", "getArticleThread"), null, 1);
        assertEquals(expected, operation.invoke(call, new GetArticleThreadInput(1)).getBranch());
    }
}
