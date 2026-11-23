// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.junit.jupiter.api.Assertions.fail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.RetryException;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpRequest;
import java.io.File;
import java.io.InputStream;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.PublicKey;
import java.security.Signature;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.stream.Stream;
import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;

/** Replays Go's refresh agreement cases for the connectors with Java ports. */
class RefreshAgreementTest {
    private static final ObjectMapper MAPPER = new ObjectMapper();
    private static final String[][] CONNECTORS = {
        {"google-gmail", "connectors/google/gmail/python/tests/refresh_agreement_cases.json"},
        {"slack", "connectors/slack/python/tests/refresh_agreement_cases.json"},
    };
    private static final List<String> STANDARD_CODES = java.util.Arrays.asList(
            "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client",
            "unsupported_grant_type", "invalid_scope", "access_denied", "interaction_required",
            "consent_required", "temporarily_unavailable", "server_error");

    @TestFactory
    Stream<DynamicTest> replaysGoRefreshAgreement() throws Exception {
        final List<DynamicTest> tests = new ArrayList<>();
        for (String[] connector : CONNECTORS) {
            final JsonNode declaration = declaration(connector[0]);
            final JsonNode cases = MAPPER.readTree(new File(repositoryRoot(), connector[1]));
            assertTrue(cases.size() > 0);
            for (JsonNode testCase : cases) {
                tests.add(DynamicTest.dynamicTest(connector[0] + ": " + testCase.get("name").asText(),
                        () -> replay(declaration, testCase)));
                if (testCase.has("tokenExchangeTimeout")) {
                    tests.add(DynamicTest.dynamicTest(connector[0] + " timeout: " + testCase.get("name").asText(),
                            () -> replayExchangeTimeout(declaration, testCase)));
                }
            }
        }
        return tests.stream();
    }

    private static void replay(final JsonNode declaration, final JsonNode testCase) throws Exception {
        final JsonNode authorization = declaration.get("authorization");
        KeyPair keyPair = null;
        String privateKeyPem = "";
        if (testCase.hasNonNull("keyCredential") || testCase.hasNonNull("privateKeyCredential")) {
            final KeyPairGenerator generator = KeyPairGenerator.getInstance("RSA");
            generator.initialize(2048);
            keyPair = generator.generateKeyPair();
            privateKeyPem = "-----BEGIN PRIVATE KEY-----\n"
                    + Base64.getMimeEncoder().encodeToString(keyPair.getPrivate().getEncoded())
                    + "\n-----END PRIVATE KEY-----\n";
        }
        final MapCredentials credentials = credentialsFor(declaration, testCase, privateKeyPem, Collections.emptyMap());
        final Map<String, String> initial = new LinkedHashMap<>(credentials.values);
        final RecordingTransport transport = new RecordingTransport();
        transport.body = MAPPER.writeValueAsBytes(testCase.get("response"));
        final ManifestCredentialRefreshDriver<MapCredentials> driver = new ManifestCredentialRefreshDriver<>(
                "Agreement", authorization, MapCredentials.accessor(fieldNames(declaration)), transport,
                settings(testCase), Instant::now);
        final Instant now = Instant.now();
        final CredentialRefreshDriver.State<MapCredentials> state =
                new CredentialRefreshDriver.State<>(credentials, Instant.parse("2000-01-01T00:00:00Z"), now);
        for (JsonNode refreshCase : testCase.path("refreshRequiredCases")) {
            final String expiresAtName = refreshCase.get("expiresAt").asText();
            final Instant expiresAt = "fresh".equals(expiresAtName) ? now.plus(Duration.ofHours(1))
                    : "expired".equals(expiresAtName) ? now.minus(Duration.ofHours(1)) : null;
            final Map<String, String> overrides = new LinkedHashMap<>();
            final Iterator<Map.Entry<String, JsonNode>> fields = refreshCase.path("credentials").fields();
            while (fields.hasNext()) {
                final Map.Entry<String, JsonNode> field = fields.next();
                overrides.put(field.getKey(), field.getValue().asText());
            }
            if (refreshCase.hasNonNull("jwtAccessTokenCredential")) {
                final Duration offset = "fresh".equals(refreshCase.get("jwtExpiry").asText())
                        ? Duration.ofHours(1) : Duration.ofHours(-1);
                final String payload = Base64.getUrlEncoder().withoutPadding().encodeToString(
                        ("{\"exp\": " + now.plus(offset).getEpochSecond() + "}").getBytes(StandardCharsets.UTF_8));
                overrides.put(refreshCase.get("jwtAccessTokenCredential").asText(), "e30." + payload + ".signature");
            }
            final MapCredentials stored = credentialsFor(declaration, testCase, privateKeyPem, overrides);
            assertEquals(refreshCase.get("expected").asBoolean(),
                    driver.isRefreshRequired(new CredentialRefreshDriver.State<>(stored, expiresAt, now)),
                    refreshCase.get("name").asText());
            assertTrue(transport.requests.isEmpty());
        }
        final Instant startedAt = Instant.now();
        final CredentialRefreshDriver.Result<MapCredentials> result;
        try {
            result = driver.refresh(state);
        } catch (RuntimeException error) {
            if (testCase.path("mayRefuseWithoutCall").asBoolean(false) && transport.requests.isEmpty()) {
                assertTrue(error instanceof ReauthorizationRequiredException, error.toString());
                return;
            }
            if (testCase.path("mayRefuseWithoutCall").asBoolean(false)) {
                fail("a blank client secret reached the provider");
            }
            assertTrue(testCase.hasNonNull("expectedError"), "unexpected refresh error: " + error);
            if ("reauthorization".equals(testCase.get("expectedError").asText())) {
                assertTrue(error instanceof ReauthorizationRequiredException, error.toString());
            } else {
                assertTrue(error instanceof RetryException, error.toString());
            }
            assertEquals(1, transport.requests.size());
            checkRequest(testCase, transport.requests.get(0), keyPair == null ? null : keyPair.getPublic());
            return;
        }
        assertFalse(testCase.hasNonNull("expectedError"), "refresh unexpectedly succeeded");
        final Instant finishedAt = Instant.now();
        assertEquals(1, transport.requests.size());
        checkRequest(testCase, transport.requests.get(0), keyPair == null ? null : keyPair.getPublic());
        final Iterator<Map.Entry<String, JsonNode>> updates = testCase.path("updates").fields();
        while (updates.hasNext()) {
            final Map.Entry<String, JsonNode> update = updates.next();
            initial.put(update.getKey(), update.getValue().asText());
        }
        assertEquals(initial, result.getCredentials().values);
        final JsonNode expectedExpiry = testCase.get("expectedExpiryMilliseconds");
        if (expectedExpiry != null && !expectedExpiry.isNull() && expectedExpiry.asLong() == 0) {
            assertEquals(ManifestCredentialRefreshDriver.NO_EXPIRY, result.getExpiresAt());
        } else if (expectedExpiry != null && !expectedExpiry.isNull()) {
            final Duration lifetime = Duration.ofMillis(expectedExpiry.asLong());
            assertFalse(result.getExpiresAt().isBefore(startedAt.plus(lifetime)));
            assertFalse(result.getExpiresAt().isAfter(finishedAt.plus(lifetime)));
        } else {
            assertTrue(result.getExpiresAt().isAfter(now));
        }
        final List<String> declaredTerminal = new ArrayList<>();
        for (JsonNode code : testCase.path("terminalErrorCodes")) {
            declaredTerminal.add(code.asText());
        }
        if (!declaredTerminal.isEmpty()) {
            final Set<String> codes = new LinkedHashSet<>(STANDARD_CODES);
            codes.addAll(declaredTerminal);
            for (String code : codes) {
                transport.requests.clear();
                transport.body = ("{\"error\":\"" + code + "\"}").getBytes(StandardCharsets.UTF_8);
                transport.statusCode = 400;
                final Class<? extends RuntimeException> expected = declaredTerminal.contains(code)
                        ? ReauthorizationRequiredException.class : RetryException.class;
                assertThrows(expected, () -> driver.refresh(state), code);
                assertEquals(1, transport.requests.size());
            }
        }
    }

    private static void replayExchangeTimeout(final JsonNode declaration, final JsonNode testCase) throws Exception {
        final ObjectNode authorization = declaration.get("authorization").deepCopy();
        final ObjectNode grant = selectedTimeoutGrant(authorization, testCase);
        final Map<String, Object> settings = settings(testCase);
        final JsonNode timeoutSource = grant.get("tokenExchangeTimeout");
        final ManifestCredentialRefreshDriver<MapCredentials> original = new ManifestCredentialRefreshDriver<>(
                "Agreement", declaration.get("authorization"), MapCredentials.accessor(fieldNames(declaration)),
                new RecordingTransport(), settings, Instant::now);
        assertEquals(Duration.ofNanos(testCase.get("tokenExchangeTimeoutNanoseconds").asLong()),
                original.tokenExchangeTimeout(timeoutSource));
        final RecordingTransport transport = new RecordingTransport();
        if (timeoutSource.isObject()) {
            settings.put(timeoutSource.get("fromSetting").asText(), "1s");
            transport.bodyDelay = Duration.ofMillis(1200);
        } else {
            grant.put("tokenExchangeTimeout", "10ms");
            transport.bodyDelay = Duration.ofMillis(500);
        }
        transport.body = MAPPER.writeValueAsBytes(testCase.get("response"));
        final ManifestCredentialRefreshDriver<MapCredentials> driver = new ManifestCredentialRefreshDriver<>(
                "Agreement", authorization, MapCredentials.accessor(fieldNames(declaration)), transport,
                settings, Instant::now);
        final MapCredentials credentials = credentialsFor(declaration, testCase, "", Collections.emptyMap());
        assertThrows(RetryException.class, () -> driver.refresh(new CredentialRefreshDriver.State<>(
                credentials, Instant.parse("2000-01-01T00:00:00Z"), Instant.now())));
        assertEquals(1, transport.requests.size());
    }

    private static ObjectNode selectedTimeoutGrant(final ObjectNode authorization, final JsonNode testCase) {
        JsonNode method = authorization;
        for (JsonNode candidate : authorization.path("methods")) {
            if (candidate.path("id").asText().equals(testCase.path("methodId").asText(null))) {
                method = candidate;
            }
        }
        if ("client_credentials".equals(testCase.path("parameters").path("grant_type").asText())) {
            return (ObjectNode) method.get("clientCredentials");
        }
        return (ObjectNode) method.get("oauth2").get("refresh");
    }

    private static void checkRequest(final JsonNode testCase, final ProviderHttpRequest request, final PublicKey publicKey)
            throws Exception {
        assertEquals(testCase.get("endpoint").asText(), request.getUrl());
        final String contentType = request.getHeaders().get("Content-Type");
        final Map<String, String> parameters = new HashMap<>();
        if ("json".equals(testCase.get("encoding").asText())) {
            assertTrue(contentType.startsWith("application/json"));
            final Iterator<Map.Entry<String, JsonNode>> fields = MAPPER.readTree(request.getBody()).fields();
            while (fields.hasNext()) {
                final Map.Entry<String, JsonNode> field = fields.next();
                parameters.put(field.getKey(), field.getValue().asText());
            }
        } else {
            assertTrue(contentType.startsWith("application/x-www-form-urlencoded"));
            for (String pair : new String(request.getBody(), StandardCharsets.UTF_8).split("&")) {
                final int equals = pair.indexOf('=');
                final String name = URLDecoder.decode(pair.substring(0, equals), "UTF-8");
                if (!parameters.containsKey(name)) {
                    parameters.put(name, URLDecoder.decode(pair.substring(equals + 1), "UTF-8"));
                }
            }
        }
        final Map<String, String> expected = new HashMap<>();
        final Iterator<Map.Entry<String, JsonNode>> declared = testCase.get("parameters").fields();
        while (declared.hasNext()) {
            final Map.Entry<String, JsonNode> field = declared.next();
            expected.put(field.getKey(), field.getValue().asText());
        }
        final String authorizationHeader = request.getHeaders().get("Authorization");
        if ("client_secret_basic".equals(testCase.get("clientAuth").asText())) {
            assertNotNull(authorizationHeader);
            assertTrue(authorizationHeader.startsWith("Basic "));
            assertEquals(testCase.get("clientId").asText() + ":" + testCase.get("clientSecret").asText(),
                    new String(Base64.getDecoder().decode(authorizationHeader.substring(6)), StandardCharsets.UTF_8));
        } else {
            assertTrue(authorizationHeader == null || authorizationHeader.isEmpty());
        }
        if (testCase.hasNonNull("assertion")) {
            final String assertion = parameters.get("assertion");
            final String[] segments = assertion.split("\\.");
            assertEquals(3, segments.length);
            assertNotNull(publicKey);
            final Signature signature = Signature.getInstance("SHA256withRSA");
            signature.initVerify(publicKey);
            signature.update((segments[0] + "." + segments[1]).getBytes(StandardCharsets.US_ASCII));
            assertTrue(signature.verify(Base64.getUrlDecoder().decode(segments[2])));
            final JsonNode claims = MAPPER.readTree(Base64.getUrlDecoder().decode(segments[1]));
            final JsonNode declaredAssertion = testCase.get("assertion");
            assertEquals(declaredAssertion.get("issuer").asText(), claims.get("iss").asText());
            assertEquals(declaredAssertion.get("audience").asText(), claims.get("aud").asText());
            assertEquals(declaredAssertion.path("subject").asText(""), claims.path("sub").asText(""));
            assertEquals(declaredAssertion.path("scope").asText(""), claims.path("scope").asText(""));
            assertEquals(declaredAssertion.get("lifetimeSeconds").asLong(),
                    claims.get("exp").asLong() - claims.get("iat").asLong());
            expected.put("assertion", assertion);
        }
        assertEquals(expected, parameters);
    }

    private static MapCredentials credentialsFor(
            final JsonNode declaration, final JsonNode testCase, final String privateKeyPem,
            final Map<String, String> overrides) throws Exception {
        final Map<String, String> material = new LinkedHashMap<>();
        final Iterator<Map.Entry<String, JsonNode>> fields = testCase.path("credentials").fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> field = fields.next();
            material.put(field.getKey(), field.getValue().asText());
        }
        material.putAll(overrides);
        if (testCase.hasNonNull("keyCredential")) {
            final ObjectNode keyMembers = testCase.get("keyMembers").deepCopy();
            keyMembers.put(testCase.get("privateKeyMember").asText(), privateKeyPem);
            material.put(testCase.get("keyCredential").asText(), MAPPER.writeValueAsString(keyMembers));
        }
        if (testCase.hasNonNull("privateKeyCredential")) {
            material.put(testCase.get("privateKeyCredential").asText(), privateKeyPem);
        }
        final Set<String> names = fieldNames(declaration);
        final Map<String, String> values = new LinkedHashMap<>();
        for (Map.Entry<String, String> entry : material.entrySet()) {
            if (names.contains(entry.getKey())) {
                values.put(entry.getKey(), entry.getValue());
            }
        }
        final boolean hasMethods = declaration.get("authorization").path("methods").size() > 0;
        return new MapCredentials(values, hasMethods ? testCase.path("methodId").asText("") : null);
    }

    private static Map<String, Object> settings(final JsonNode testCase) {
        final Map<String, Object> settings = new HashMap<>();
        final Iterator<Map.Entry<String, JsonNode>> fields = testCase.path("settings").fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> field = fields.next();
            settings.put(field.getKey(), field.getValue().isIntegralNumber()
                    ? (Object) field.getValue().longValue() : field.getValue().asText());
        }
        return settings;
    }

    private static Set<String> fieldNames(final JsonNode declaration) {
        final Set<String> names = new HashSet<>();
        declaration.get("credentialFields").fieldNames().forEachRemaining(names::add);
        return names;
    }

    private static JsonNode declaration(final String connector) throws Exception {
        try (InputStream stream = RefreshAgreementTest.class.getResourceAsStream(
                "/oauthtoken/" + connector + "-authorization.json")) {
            return MAPPER.readTree(stream);
        }
    }

    private static File repositoryRoot() {
        return new File(System.getProperty("user.dir")).getParentFile();
    }
}
