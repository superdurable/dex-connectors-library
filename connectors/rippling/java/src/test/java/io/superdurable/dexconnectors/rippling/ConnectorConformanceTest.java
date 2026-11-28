// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Paths;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

class ConnectorConformanceTest {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final String ORIGIN = "https://rest.ripplingapis.com";
    private static final String FIXTURE = "../../../conformance/connectors/rippling/scenarios.json";

    @TestFactory
    protected Iterable<DynamicTest> replayPublicQueries() throws IOException {
        String selected = System.getenv().getOrDefault("CONNECTOR_CONFORMANCE_FIXTURE", FIXTURE);
        JsonNode scenarios = JSON.readTree(Paths.get(selected).toFile()).get("scenarios");
        assertTrue(scenarios.size() > 0);
        List<DynamicTest> tests = new java.util.ArrayList<>();
        for (JsonNode scenario : scenarios) {
            tests.add(
                    DynamicTest.dynamicTest(scenario.get("name").asText(), () -> replay(scenario)));
        }
        assertFalse(tests.isEmpty());
        return tests;
    }

    private void replay(JsonNode scenario) throws Exception {
        String name = scenario.get("name").asText();
        assertFalse(scenario.has("pending"), name + " is pending");
        String operation = scenario.get("operation").asText();
        RecordedProvider provider = new RecordedProvider(scenario.get("exchanges"), ORIGIN);
        Call call =
                Call.create(
                        CallId.of(scenario.get("callId").asText()),
                        new ConnectionRef("rippling", "rippling"),
                        new OperationRef("rippling", operation),
                        null,
                        1);
        JsonNode input = scenario.get("input");
        RipplingClient client =
                new RipplingClient(
                        ignored -> new SecretString("sample-rippling-api-token"),
                        ORIGIN,
                        provider,
                        name.endsWith("-smallLimit") ? 16 : 1 << 20);
        QueryAttempt<?> attempt;
        switch (operation) {
            case "getWorker":
                attempt =
                        client.getWorker()
                                .invoke(call, JSON.treeToValue(input, GetWorkerInput.class));
                break;
            case "listWorkers":
                attempt =
                        client.listWorkers()
                                .invoke(call, JSON.treeToValue(input, ListWorkersInput.class));
                break;
            default:
                throw new AssertionError(operation);
        }
        JsonNode expected = scenario.get("expected");
        assertEquals(expected.get("branch").asText(), attempt.getBranch());
        assertEquals(expected.get("retry").asBoolean(), attempt.isRetry());
        assertFalse(expected.get("uncertain").asBoolean());
        assertEquals(expected.get("retryAfterMs").asLong(), attempt.getRetryAfter().toMillis());
        assertEquals(
                expected.get("failureKind").asText(),
                attempt.getFailure() == null ? "" : attempt.getFailure().getKind().name());
        if (expected.has("output"))
            assertEquals(
                    expected.get("output"),
                    JSON.readTree(JSON.writeValueAsBytes(attempt.getValue())));
        assertEquals(expected.get("requestCount").asInt(), provider.sent);
        assertEquals(provider.exchanges.size(), provider.attempted);
    }

    private static final class RecordedProvider implements ProviderHttpTransport {
        final JsonNode exchanges;
        int attempted;
        int sent;
        final String origin;

        RecordedProvider(JsonNode exchanges, String origin) {
            this.exchanges = exchanges;
            this.origin = origin;
        }

        @Override
        public ProviderHttpResponse send(ProviderHttpRequest request) throws IOException {
            JsonNode exchange = exchanges.get(attempted++);
            assertNotNull(exchange, "unexpected request");
            assertEquals("absent", exchange.get("keyMode").asText());
            assertEquals(exchange.get("method").asText(), request.getMethod());
            String[] url = request.getUrl().split("\\?", 2);
            assertEquals(origin + exchange.get("path").asText(), url[0]);
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            if (url.length == 2) {
                for (String pair : url[1].split("&")) {
                    String[] parts = pair.split("=", 2);
                    String key = java.net.URLDecoder.decode(parts[0], "UTF-8");
                    parameters
                            .computeIfAbsent(key, ignored -> new java.util.ArrayList<>())
                            .add(java.net.URLDecoder.decode(parts[1], "UTF-8"));
                }
            }
            assertEquals(
                    exchange.has("query") ? exchange.get("query") : JSON.createObjectNode(),
                    JSON.valueToTree(parameters));
            Map<String, List<String>> headers = new LinkedHashMap<>();
            request.getHeaders()
                    .forEach((name, value) -> headers.put(name, Collections.singletonList(value)));
            assertEquals(exchange.get("requestHeaders"), JSON.valueToTree(headers));
            assertTrue(request.getBody() == null || request.getBody().length == 0);
            if ("before-send".equals(exchange.path("transportFailure").asText()))
                throw new IOException("fixture");
            sent++;
            if ("after-send".equals(exchange.path("transportFailure").asText()))
                throw new IOException("fixture");
            Map<String, List<String>> responseHeaders = new LinkedHashMap<>();
            exchange.path("headers")
                    .fields()
                    .forEachRemaining(entry ->
                            responseHeaders.put(
                                    entry.getKey(),
                                    Collections.singletonList(entry.getValue().asText())));
            return ProviderHttpResponse.buffered(
                    exchange.get("status").asInt(),
                    new ProviderHttpHeaders(responseHeaders),
                    exchange.path("body").asText("").getBytes(StandardCharsets.UTF_8));
        }
    }
}
