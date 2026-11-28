// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.conformance;

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

/** Replay recorded model-list HTTP exchanges and compare complete Query outcomes. */
public abstract class ModelCatalogScenarios {
    private final String fixture;
    private final String origin;
    private final String connector;
    private final String key;
    private final String missingKeyCase;
    private final String smallBodyCase;

    protected ModelCatalogScenarios(String fixture, String origin, String connector, String key, String missingKeyCase, String smallBodyCase) {
        this.fixture = fixture;
        this.origin = origin;
        this.connector = connector;
        this.key = key;
        this.missingKeyCase = missingKeyCase;
        this.smallBodyCase = smallBodyCase;
    }

    protected abstract QueryAttempt<?> invoke(Call call, JsonNode input, CredentialProvider<SecretString> credentials,
                                              ProviderHttpTransport transport, int limit) throws Exception;

    private static final ObjectMapper JSON = new ObjectMapper();

    @TestFactory protected Iterable<DynamicTest> replayModelCatalog() throws IOException {
        String selected = System.getenv().getOrDefault("CONNECTOR_CONFORMANCE_FIXTURE", fixture);
        JsonNode scenarios = JSON.readTree(Paths.get(selected).toFile()).get("scenarios");
        assertTrue(scenarios.size() > 0);
        List<DynamicTest> tests = new java.util.ArrayList<>();
        for (JsonNode scenario : scenarios) {
            tests.add(DynamicTest.dynamicTest(scenario.get("name").asText(), () -> replay(scenario)));
        }
        return tests;
    }

    private void replay(JsonNode scenario) throws Exception {
        String name = scenario.get("name").asText();
        assertFalse(scenario.has("pending"), name + " is pending");
        assertEquals("listModels", scenario.get("operation").asText());
        RecordedProvider provider = new RecordedProvider(scenario.get("exchanges"));
        Call call = Call.create(CallId.of(scenario.get("callId").asText()), new ConnectionRef(connector, connector),
                new OperationRef(connector, "listModels"), null, 1);
        QueryAttempt<?> attempt = invoke(call, scenario.get("input"),
                ignored -> new SecretString(missingKeyCase.equals(name) ? "" : key), provider,
                smallBodyCase.equals(name) ? 16 : 1 << 20);
        JsonNode expected = scenario.get("expected");
        assertEquals(expected.get("branch").asText(), attempt.getBranch());
        assertEquals(expected.get("retry").asBoolean(), attempt.isRetry());
        assertFalse(expected.get("uncertain").asBoolean());
        assertEquals(expected.get("retryAfterMs").asLong(), attempt.getRetryAfter().toMillis());
        assertEquals(expected.get("failureKind").asText(), attempt.getFailure() == null ? "" : attempt.getFailure().getKind().name());
        if (expected.has("output")) assertEquals(expected.get("output"), JSON.valueToTree(attempt.getValue()));
        assertEquals(expected.get("requestCount").asInt(), provider.sent);
        assertEquals(provider.exchanges.size(), provider.attempted);
    }

    private final class RecordedProvider implements ProviderHttpTransport {
        final JsonNode exchanges;
        int attempted;
        int sent;
        RecordedProvider(JsonNode exchanges) { this.exchanges = exchanges; }

        @Override public ProviderHttpResponse send(ProviderHttpRequest request) throws IOException {
            JsonNode exchange = exchanges.get(attempted++);
            assertNotNull(exchange, "unexpected request");
            assertEquals("absent", exchange.get("keyMode").asText());
            assertEquals(exchange.get("method").asText(), request.getMethod());
            assertEquals(origin + exchange.get("path").asText(), request.getUrl());
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
