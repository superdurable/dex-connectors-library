// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import io.superdurable.dexconnectors.hackernews.StoryFeed.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Paths;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;
import static org.junit.jupiter.api.Assertions.*;

class ConnectorConformanceTest {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final String ORIGIN = "https://hacker-news.firebaseio.com/v0";
    private static final String FIXTURE = "../../../conformance/connectors/hackernews/scenarios.json";

    @TestFactory protected Iterable<DynamicTest> replayStoryFeeds() throws IOException {
        String selected = System.getenv().getOrDefault("CONNECTOR_CONFORMANCE_FIXTURE", FIXTURE);
        JsonNode scenarios = JSON.readTree(Paths.get(selected).toFile()).get("scenarios");
        assertTrue(scenarios.size() > 0);
        List<DynamicTest> tests = new java.util.ArrayList<>();
        for (JsonNode scenario : scenarios) {
            if (!"listStoryIDs".equals(scenario.get("operation").asText())) continue;
            tests.add(DynamicTest.dynamicTest(scenario.get("name").asText(), () -> replay(scenario)));
        }
        assertFalse(tests.isEmpty());
        return tests;
    }

    private void replay(JsonNode scenario) throws Exception {
        String name = scenario.get("name").asText();
        assertFalse(scenario.has("pending"), name + " is pending");
        RecordedProvider provider = new RecordedProvider(scenario.get("exchanges"));
        Call call = Call.create(CallId.of(scenario.get("callId").asText()), new ConnectionRef("hacker-news", "hacker-news-public"),
                new OperationRef("hacker-news-daily", "listStoryIDs"), null, 1);
        Clock clock = Clock.fixed(Instant.parse(scenario.get("fixedClock").asText()), ZoneOffset.UTC);
        ListStoryIDsOperation operation = new ListStoryIDsOperation(ORIGIN, provider, 1 << 20, clock);
        QueryAttempt<?> attempt = operation.invoke(call, JSON.treeToValue(scenario.get("input"), ListStoryIDsInput.class));
        JsonNode expected = scenario.get("expected");
        assertEquals(expected.get("branch").asText(), attempt.getBranch());
        assertEquals(expected.get("retry").asBoolean(), attempt.isRetry());
        assertFalse(expected.get("uncertain").asBoolean());
        assertEquals(expected.get("retryAfterMs").asLong(), attempt.getRetryAfter().toMillis());
        assertEquals(expected.get("failureKind").asText(), attempt.getFailure() == null ? "" : attempt.getFailure().getKind().name());
        if (expected.has("output")) assertEquals(expected.get("output"), JSON.readTree(JSON.writeValueAsBytes(attempt.getValue())));
        assertEquals(expected.get("requestCount").asInt(), provider.sent);
        assertEquals(provider.exchanges.size(), provider.attempted);
    }

    private static final class RecordedProvider implements ProviderHttpTransport {
        final JsonNode exchanges;
        int attempted;
        int sent;
        RecordedProvider(JsonNode exchanges) { this.exchanges = exchanges; }

        @Override public ProviderHttpResponse send(ProviderHttpRequest request) throws IOException {
            JsonNode exchange = exchanges.get(attempted++);
            assertNotNull(exchange, "unexpected request");
            assertEquals("absent", exchange.get("keyMode").asText());
            assertEquals(exchange.get("method").asText(), request.getMethod());
            assertEquals(ORIGIN + exchange.get("path").asText(), request.getUrl());
            Map<String, List<String>> headers = new LinkedHashMap<>();
            request.getHeaders().forEach((name, value) -> headers.put(name, Collections.singletonList(value)));
            assertEquals(exchange.get("requestHeaders"), JSON.valueToTree(headers));
            assertTrue(request.getBody() == null || request.getBody().length == 0);
            if ("before-send".equals(exchange.path("transportFailure").asText())) throw new IOException("fixture");
            sent++;
            if ("after-send".equals(exchange.path("transportFailure").asText())) throw new IOException("fixture");
            Map<String, List<String>> responseHeaders = new LinkedHashMap<>();
            exchange.path("headers").fields().forEachRemaining(entry ->
                    responseHeaders.put(entry.getKey(), Collections.singletonList(entry.getValue().asText())));
            return ProviderHttpResponse.buffered(exchange.get("status").asInt(), new ProviderHttpHeaders(responseHeaders),
                    exchange.path("body").asText("").getBytes(StandardCharsets.UTF_8));
        }
    }
}
