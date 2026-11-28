// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.oauthtoken.ManifestCredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.*;

import java.io.InputStream;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.time.*;
import java.util.*;

class RefreshAgreementTest {
    private static final ObjectMapper JSON = new ObjectMapper();

    @TestFactory
    Iterable<DynamicTest> replayManifestGrants() throws Exception {
        List<DynamicTest> tests = new ArrayList<>();
        try (InputStream cases = getClass().getResourceAsStream("/refresh-agreement-cases.json")) {
            for (JsonNode scenario : JSON.readTree(Objects.requireNonNull(cases)))
                tests.add(
                        DynamicTest.dynamicTest(
                                scenario.path("name").asText(), () -> checkGrant(scenario)));
        }
        assertFalse(tests.isEmpty());
        return tests;
    }

    private void checkGrant(JsonNode scenario) throws Exception {
        Instant now = Instant.now();
        JsonNode authorization;
        try (InputStream resource =
                getClass()
                        .getResourceAsStream(
                                "/META-INF/dex-connectors/workday/refresh-authorization.json")) {
            authorization = JSON.readTree(Objects.requireNonNull(resource));
        }
        ManifestCredentialRefreshDriver<WorkdayCredentials> driver =
                new ManifestCredentialRefreshDriver<>(
                        "Workday",
                        authorization,
                        WorkdayCredentials.FIELDS,
                        request -> {
                            assertEquals(scenario.path("endpoint").asText(), request.getUrl());
                            assertEquals(
                                    Duration.ofNanos(
                                            scenario.path("tokenExchangeTimeoutNanoseconds")
                                                    .longValue()),
                                    request.getTimeout());
                            assertEquals("POST", request.getMethod());
                            ObjectNode parameters = JSON.createObjectNode();
                            for (String pair :
                                    new String(request.getBody(), StandardCharsets.UTF_8)
                                            .split("&")) {
                                String[] fields = pair.split("=", 2);
                                parameters.put(
                                        URLDecoder.decode(fields[0], "UTF-8"),
                                        URLDecoder.decode(fields[1], "UTF-8"));
                            }
                            assertEquals(scenario.get("parameters"), parameters);
                            return ProviderHttpResponse.buffered(
                                    200,
                                    ProviderHttpHeaders.EMPTY,
                                    JSON.writeValueAsBytes(scenario.get("response")));
                        },
                        Collections.singletonMap(
                                "tokenEndpoint",
                                scenario.path("settings").path("tokenEndpoint").asText()),
                        () -> now);
        JsonNode fields = scenario.get("credentials");
        WorkdayCredentials credentials =
                new WorkdayCredentials(
                        fields.path("oauth_client_id").asText(),
                        new SecretString(fields.path("oauth_client_secret").asText()),
                        new SecretString(fields.path("access_token").asText()),
                        new SecretString(fields.path("refresh_token").asText()));
        for (JsonNode decision : scenario.get("refreshRequiredCases")) {
            Map<String, String> changes = new HashMap<>();
            decision.path("credentials")
                    .fields()
                    .forEachRemaining(
                            change -> changes.put(change.getKey(), change.getValue().asText()));
            String expiry = decision.path("expiresAt").asText();
            Instant expiresAt =
                    expiry.equals("fresh")
                            ? now.plusSeconds(3600)
                            : expiry.equals("expired") ? now.minusSeconds(1) : null;
            assertEquals(
                    decision.path("expected").asBoolean(),
                    driver.isRefreshRequired(
                            new CredentialRefreshDriver.State<>(
                                    WorkdayCredentials.FIELDS.withCredentialFields(
                                            credentials, changes),
                                    expiresAt,
                                    now)));
        }
        CredentialRefreshDriver.State<WorkdayCredentials> state =
                new CredentialRefreshDriver.State<>(credentials, null, now);
        switch (scenario.path("expectedError").asText("")) {
            case "reauthorization":
                assertThrows(ReauthorizationRequiredException.class, () -> driver.refresh(state));
                break;
            case "retryable":
                assertThrows(RetryException.class, () -> driver.refresh(state));
                break;
            case "":
                CredentialRefreshDriver.Result<WorkdayCredentials> result = driver.refresh(state);
                for (String name : WorkdayCredentials.FIELDS.getCredentialFieldNames()) {
                    String expected =
                            scenario.path("updates").path(name).asText(fields.path(name).asText());
                    assertEquals(
                            expected,
                            WorkdayCredentials.FIELDS.readCredentialField(
                                    result.getCredentials(), name));
                }
                assertEquals(
                        now.plusSeconds(scenario.path("response").path("expires_in").longValue()),
                        result.getExpiresAt());
                break;
            default:
                throw new AssertionError("unknown refresh result");
        }
    }
}
