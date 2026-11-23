// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.RetryException;
import io.superdurable.dexconnectors.sdk.SecretString;
import java.nio.charset.StandardCharsets;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.Signature;
import java.security.interfaces.RSAPrivateCrtKey;
import java.time.Duration;
import java.time.Instant;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import org.junit.jupiter.api.Test;

class ManifestRefreshTest {
    private static final ObjectMapper MAPPER = new ObjectMapper();
    private static final Set<String> FIELDS = new HashSet<>(Arrays.asList(
            "access_token", "refresh_token", "client_id", "client_secret", "instance_url"));
    private static final String AUTH = "{\"defaultMethod\":\"oauth\",\"methods\":[{\"id\":\"oauth\",\"type\":\"oauth2\","
            + "\"fields\":[{\"name\":\"access_token\",\"type\":\"secretString\"},{\"name\":\"refresh_token\","
            + "\"type\":\"secretString\"},{\"name\":\"client_id\",\"type\":\"string\"},{\"name\":\"client_secret\","
            + "\"type\":\"secretString\"},{\"name\":\"instance_url\",\"type\":\"url\"}],\"oauth2\":{\"tokenEndpoint\":"
            + "\"https://oauth.example.test/token\",\"tokenEndpointAuthMethod\":\"client_secret_basic\","
            + "\"tokenRequestEncoding\":\"json\",\"clientIDCredential\":\"client_id\",\"clientSecretCredential\":"
            + "\"client_secret\",\"scopes\":[\"offline_access\",\"files.read\"],\"credentialMappings\":[{\"credential\":"
            + "\"access_token\",\"source\":\"access_token\"},{\"credential\":\"refresh_token\",\"source\":"
            + "\"refresh_token\"},{\"credential\":\"instance_url\",\"source\":\"instance_url\"}],\"refresh\":{"
            + "\"terminalErrorCodes\":[\"invalid_grant\",\"invalid_client\"],\"additionalParameters\":{\"scope\":"
            + "\"offline_access files.read\"}}}},{\"id\":\"static\",\"type\":\"apiKey\",\"fields\":[]}]}";

    @Test
    void rotatesCompleteMaterialAndSendsJsonWithBasicAuthentication() throws Exception {
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"refresh_token\":\"rotated\","
                + "\"expires_in\":3600,\"token_type\":\"bearer\",\"scope\":\"files.read\"}");
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth(), transport);
        final CredentialRefreshDriver.Result<MapCredentials> result = driver.refresh(state(credentials(), null));
        assertEquals("next", result.getCredentials().values.get("access_token"));
        assertEquals("rotated", result.getCredentials().values.get("refresh_token"));
        assertEquals("client-id", result.getCredentials().values.get("client_id"));
        assertTrue(result.getExpiresAt().isAfter(Instant.now().plus(Duration.ofMinutes(59))));
        final JsonNode body = MAPPER.readTree(transport.requests.get(0).getBody());
        assertEquals("refresh_token", body.get("grant_type").asText());
        assertEquals("offline_access files.read", body.get("scope").asText());
        assertEquals("Basic " + Base64.getEncoder().encodeToString("client-id:client-secret".getBytes(StandardCharsets.UTF_8)),
                transport.requests.get(0).getHeaders().get("Authorization"));
    }

    @Test
    void keepsUnreturnedRefreshTokenAndClassifiesErrors() throws Exception {
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"expires_in\":60,\"token_type\":\"Bearer\"}");
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth(), transport);
        assertEquals("prior", driver.refresh(state(credentials(), null)).getCredentials().values.get("refresh_token"));
        transport.statusCode = 400;
        transport.body = "{\"error\":\"invalid_grant\"}".getBytes(StandardCharsets.UTF_8);
        assertThrows(ReauthorizationRequiredException.class, () -> driver.refresh(state(credentials(), null)));
        transport.statusCode = 503;
        assertThrows(RetryException.class, () -> driver.refresh(state(credentials(), null)));
        transport.statusCode = 500;
        transport.body = new byte[0];
        final RetryException error = assertThrows(RetryException.class, () -> driver.refresh(state(credentials(), null)));
        assertEquals("Example token endpoint returned HTTP 500", error.getFailure().getMessage());
    }

    @Test
    void staticMethodNeverRefreshesAndMultiMethodRequiresMethodIdentity() {
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth(), new RecordingTransport());
        final MapCredentials staticCredentials = new MapCredentials(credentials().values, "static");
        assertFalse(driver.isRefreshRequired(state(staticCredentials, null)));
        assertThrows(ReauthorizationRequiredException.class, () -> driver.refresh(state(staticCredentials, null)));
        final MapCredentials withoutMethod = new MapCredentials(credentials().values, null);
        assertThrows(IllegalArgumentException.class, () -> driver.isRefreshRequired(state(withoutMethod, null)));
        final MapCredentials defaultMethod = new MapCredentials(credentials().values, "");
        assertFalse(driver.isRefreshRequired(state(defaultMethod, null)));
        assertTrue(driver.isRefreshRequired(state(defaultMethod, Instant.now().minus(Duration.ofMinutes(1)))));
    }

    @Test
    void templateUsesGoPathSegmentEscaping() throws Exception {
        final ObjectNode auth = auth();
        oauth(auth).set("tokenEndpoint", MAPPER.readTree("{\"template\":\"https://oauth.example.test/{client_id}/token\"}"));
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"expires_in\":3600,\"token_type\":\"Bearer\"}");
        driver(auth, transport).refresh(state(credentials("client_id", "a:b@c&d=e+f$/g;h,i?j"), null));
        assertEquals("https://oauth.example.test/a:b@c&d=e+f$%2Fg%3Bh%2Ci%3Fj/token", transport.requests.get(0).getUrl());
    }

    @Test
    void templateRejectsAliasesAndLowercasesValidTenant() throws Exception {
        final ObjectNode auth = auth();
        oauth(auth).set("tokenEndpoint", MAPPER.readTree("{\"template\":"
                + "\"https://login.microsoftonline.com/{client_id}/oauth2/v2.0/token\",\"templateValueRules\":{"
                + "\"client_id\":{\"trimSpace\":true,\"lowercase\":true,\"maxBytes\":253,\"patterns\":["
                + "\"^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$\","
                + "\"^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\\\\.)+[A-Za-z]{2,63}$\"],"
                + "\"reject\":[\"common\",\"organizations\",\"consumers\"]}}}"));
        ((ObjectNode) oauth(auth).get("refresh")).put("trimClientID", true);
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"expires_in\":3600,\"token_type\":\"Bearer\"}");
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth, transport);
        final StringBuilder longTenant = new StringBuilder();
        for (int index = 0; index < 254; index++) {
            longTenant.append('x');
        }
        for (String tenant : Arrays.asList("common", " Organizations ", "CONSUMERS", "contoso", "a/b.com",
                longTenant.toString())) {
            assertThrows(ReauthorizationRequiredException.class,
                    () -> driver.refresh(state(credentials("client_id", tenant), null)), tenant);
        }
        assertTrue(transport.requests.isEmpty());
        driver.refresh(state(credentials("client_id", "  Contoso.OnMicrosoft.com  "), null));
        assertEquals("https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token",
                transport.requests.get(0).getUrl());
        assertEquals("Basic " + Base64.getEncoder().encodeToString(
                "Contoso.OnMicrosoft.com:client-secret".getBytes(StandardCharsets.UTF_8)),
                transport.requests.get(0).getHeaders().get("Authorization"));
    }

    @Test
    void scopeArrayGraphPrefixUrlOriginAndExpiryCap() throws Exception {
        final ObjectNode auth = auth();
        final ObjectNode refresh = (ObjectNode) oauth(auth).get("refresh");
        refresh.set("requiredScopes", MAPPER.readTree("[\"files.read\"]"));
        refresh.put("responseScopeField", "scopesArray");
        refresh.put("scopeResourcePrefix", "https://graph.microsoft.com/");
        refresh.put("caseInsensitiveScopes", true);
        refresh.put("scopeResourcePrefixCaseInsensitive", true);
        refresh.put("expiryCap", "1h");
        refresh.set("responseURLValidation", MAPPER.readTree("{\"credential\":\"instance_url\",\"allowedHosts\":"
                + "[\"example.test\"],\"requireResponse\":true,\"refreshWhenInvalid\":true}"));
        final ObjectNode body = (ObjectNode) MAPPER.readTree("{\"access_token\":\"next\",\"token_type\":\"Bearer\","
                + "\"expires_in\":7200,\"scopes\":[\"HTTPS://GRAPH.MICROSOFT.COM/FILES.READ\"],"
                + "\"instance_url\":\"https://example.test/\"}");
        final RecordingTransport transport = respond(body.toString());
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth, transport);
        assertTrue(driver.isRefreshRequired(state(credentials(), Instant.now().plus(Duration.ofHours(1)))));
        final CredentialRefreshDriver.Result<MapCredentials> result = driver.refresh(state(credentials(), null));
        assertEquals("https://example.test", result.getCredentials().values.get("instance_url"));
        assertTrue(result.getExpiresAt().isBefore(Instant.now().plusSeconds(3601)));
        for (String scopes : Arrays.asList("[\"other\"]", "[\"files.read,other\"]", "[]")) {
            body.set("scopes", MAPPER.readTree(scopes));
            transport.body = MAPPER.writeValueAsBytes(body);
            assertThrows(ReauthorizationRequiredException.class, () -> driver.refresh(state(credentials(), null)));
        }
        body.remove("scopes");
        body.put("scope", "other");
        transport.body = MAPPER.writeValueAsBytes(body);
        driver.refresh(state(credentials(), null));
        body.putNull("scopes");
        transport.body = MAPPER.writeValueAsBytes(body);
        driver.refresh(state(credentials(), null));
        body.set("scopes", MAPPER.readTree("[\"files.read\"]"));
        body.put("instance_url", "https://foreign.test");
        transport.body = MAPPER.writeValueAsBytes(body);
        assertThrows(RetryException.class, () -> driver.refresh(state(credentials(), null)));
        body.putNull("instance_url");
        transport.body = MAPPER.writeValueAsBytes(body);
        assertThrows(RetryException.class,
                () -> driver.refresh(state(credentials("instance_url", "https://example.test/"), null)));
        body.remove("instance_url");
        transport.body = MAPPER.writeValueAsBytes(body);
        assertThrows(RetryException.class,
                () -> driver.refresh(state(credentials("instance_url", "https://example.test/"), null)));
    }

    @Test
    void responseUrlRulesAndBlankRefreshFields() throws Exception {
        final ObjectNode auth = auth();
        final ObjectNode refresh = (ObjectNode) oauth(auth).get("refresh");
        refresh.put("refreshWhenCredentialBlank", "instance_url");
        refresh.set("responseURLValidation", MAPPER.readTree("{\"credential\":\"instance_url\",\"allowedHosts\":"
                + "[\"example.test\"],\"allowedHostSuffix\":\"example.test\",\"allowLoopback\":true,"
                + "\"forbidPort\":true,\"defaultURL\":\"https://example.test\",\"refreshWhenInvalid\":true}"));
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"token_type\":\"Bearer\","
                + "\"expires_in\":3600,\"scope\":\"files.read\"}");
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth, transport);
        final MapCredentials blank = credentials("instance_url", "");
        assertTrue(driver.isRefreshRequired(state(blank, Instant.now().plus(Duration.ofHours(1)))));
        assertEquals("https://example.test", driver.refresh(state(blank, null)).getCredentials().values.get("instance_url"));
        for (String origin : Arrays.asList("http://[::1]", "http://127.0.0.2")) {
            assertEquals(origin, driver.refresh(state(credentials("instance_url", origin), null))
                    .getCredentials().values.get("instance_url"));
        }
        for (String origin : Arrays.asList("https://example.test:8443", "https://example.test:443",
                "https://badexample.test", "http://127.1", "http://2130706433")) {
            assertThrows(RetryException.class, () -> driver.refresh(state(credentials("instance_url", origin), null)), origin);
        }
        assertEquals(8, transport.requests.size());
        refresh.set("responseURLValidation", MAPPER.readTree("{\"credential\":\"instance_url\",\"unexpected\":true}"));
        assertThrows(IllegalArgumentException.class, () -> driver(auth, transport));
        refresh.set("responseURLValidation", MAPPER.readTree("{\"allowAnyHTTPS\":true}"));
        assertThrows(IllegalArgumentException.class, () -> driver(auth, transport));
        refresh.set("responseURLValidation", MAPPER.createObjectNode());
        refresh.put("refreshWhenCredentialBlank", "missing");
        assertThrows(IllegalArgumentException.class, () -> driver(auth, transport));
    }

    @Test
    void zohoResponseUrlHostComparisonIsCaseSensitive() throws Exception {
        final ObjectNode auth = auth();
        ((ObjectNode) oauth(auth).get("refresh")).set("responseURLValidation", MAPPER.readTree(
                "{\"credential\":\"instance_url\",\"allowedHosts\":[\"www.zohoapis.com\",\"sandbox.zohoapis.com\"],"
                        + "\"forbidPort\":true}"));
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"token_type\":\"Bearer\","
                + "\"expires_in\":3600,\"instance_url\":\"https://WWW.zohoapis.com\"}");
        final ManifestCredentialRefreshDriver<MapCredentials> driver = driver(auth, transport);
        final MapCredentials stored = credentials("instance_url", "https://www.zohoapis.com");
        assertThrows(RetryException.class, () -> driver.refresh(state(stored, null)));
        transport.body = "{\"access_token\":\"next\",\"token_type\":\"Bearer\",\"expires_in\":3600}"
                .getBytes(StandardCharsets.UTF_8);
        assertEquals("https://www.zohoapis.com", driver.refresh(state(stored, null)).getCredentials().values.get("instance_url"));
    }

    @Test
    void badTokenEndpointSettingRetries() throws Exception {
        final ObjectNode auth = auth();
        oauth(auth).set("tokenEndpoint", MAPPER.readTree("{\"fromSetting\":\"tokenEndpoint\"}"));
        final RecordingTransport transport = new RecordingTransport();
        final ManifestCredentialRefreshDriver<MapCredentials> driver = new ManifestCredentialRefreshDriver<>(
                "Example", auth, MapCredentials.accessor(FIELDS), transport,
                Collections.<String, Object>singletonMap("tokenEndpoint", "not-a-url"), Instant::now);
        assertThrows(RetryException.class, () -> driver.refresh(state(credentials(), null)));
        assertTrue(transport.requests.isEmpty());
    }

    @Test
    void missingTokenTypeAndMissingExpiryRequireDeclarations() throws Exception {
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"expires_in\":3600}");
        assertThrows(RetryException.class, () -> driver(auth(), transport).refresh(state(credentials(), null)));
        final ObjectNode auth = auth();
        final ObjectNode refresh = (ObjectNode) oauth(auth).get("refresh");
        refresh.put("acceptsMissingTokenType", true);
        refresh.set("missingExpiry", MAPPER.readTree("{\"lifetime\":\"90m\"}"));
        transport.body = "{\"access_token\":\"next\"}".getBytes(StandardCharsets.UTF_8);
        final Instant before = Instant.now();
        final Instant expiry = driver(auth, transport).refresh(state(credentials(), null)).getExpiresAt();
        assertFalse(expiry.isBefore(before.plus(Duration.ofMinutes(90))));
        refresh.put("missingExpiry", "keep");
        assertEquals(ManifestCredentialRefreshDriver.NO_EXPIRY,
                driver(auth, transport).refresh(state(credentials(), null)).getExpiresAt());
        refresh.put("missingExpiry", "sometimes");
        assertThrows(IllegalArgumentException.class, () -> driver(auth, transport));
        refresh.put("missingExpiry", "keep");
        refresh.put("storedMissingExpiry", "never");
        assertThrows(IllegalArgumentException.class, () -> driver(auth, transport));
    }

    @Test
    void publicClientSendsOnlyClientIdWhenAllowed() throws Exception {
        final ObjectNode auth = auth();
        oauth(auth).put("tokenEndpointAuthMethod", "client_secret_post");
        oauth(auth).put("tokenRequestEncoding", "form");
        final RecordingTransport transport = respond("{\"access_token\":\"next\",\"expires_in\":3600,\"token_type\":\"Bearer\"}");
        driver(auth, transport).refresh(state(credentials(), null));
        assertEquals("client_id=client-id&client_secret=client-secret&grant_type=refresh_token&refresh_token=prior"
                + "&scope=offline_access+files.read", new String(transport.requests.get(0).getBody(), StandardCharsets.UTF_8));
        assertThrows(ReauthorizationRequiredException.class,
                () -> driver(auth, transport).refresh(state(credentials("client_secret", ""), null)));
        ((ObjectNode) oauth(auth).get("refresh")).put("allowsPublicClient", true);
        transport.requests.clear();
        driver(auth, transport).refresh(state(credentials("client_secret", ""), null));
        assertEquals("client_id=client-id&grant_type=refresh_token&refresh_token=prior&scope=offline_access+files.read",
                new String(transport.requests.get(0).getBody(), StandardCharsets.UTF_8));
        assertNull(transport.requests.get(0).getHeaders().get("Authorization"));
    }

    @Test
    void privateNetworkTokenEndpointUsesRealLoopbackHttpAndRefusesPublicHttp() throws Exception {
        final com.sun.net.httpserver.HttpServer server = com.sun.net.httpserver.HttpServer.create(
                new java.net.InetSocketAddress(java.net.InetAddress.getLoopbackAddress(), 0), 0);
        server.createContext("/token", exchange -> {
            final byte[] body = "{\"access_token\":\"next\",\"expires_in\":3600,\"token_type\":\"Bearer\"}"
                    .getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, body.length);
            try (java.io.OutputStream output = exchange.getResponseBody()) {
                output.write(body);
            }
        });
        server.start();
        try {
            final ObjectNode auth = auth();
            oauth(auth).put("tokenEndpoint", "http://127.0.0.1:" + server.getAddress().getPort() + "/token");
            oauth(auth).put("tokenEndpointNetworkPolicy", "privateNetworkHTTP");
            ((ObjectNode) oauth(auth).get("refresh")).put("tokenExchangeTimeout", "5s");
            final ManifestCredentialRefreshDriver<MapCredentials> driver = new ManifestCredentialRefreshDriver<>(
                    "Example", auth, MapCredentials.accessor(FIELDS),
                    new io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpClient());
            assertEquals("next", driver.refresh(state(credentials(), null)).getCredentials().values.get("access_token"));
            oauth(auth).put("tokenEndpoint", "http://8.8.8.8/token");
            final ManifestCredentialRefreshDriver<MapCredentials> publicDriver = new ManifestCredentialRefreshDriver<>(
                    "Example", auth, MapCredentials.accessor(FIELDS),
                    new io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpClient());
            assertThrows(io.superdurable.dexconnectors.sdk.providerhttp.AddressRefusalException.class,
                    () -> publicDriver.refresh(state(credentials(), null)));
        } finally {
            server.stop(0);
        }
    }

    @Test
    void decodesGoJwtExpiryCases() {
        final Object[][] cases = {
            {"eyJleHAiOjEyfQ", 12L}, {"eyJleHAiOjEyfQ==", 12L}, {"eyJleHAiOjEyM30=", 123L},
            {"eyJleHAiOjEyMzR9====", 1234L}, {"eyJleHAiOjEyMzR9\r\n=", 1234L}, {"eyJleHAiOjEyMzR9A", null},
            {"eyJleHAiOjEyMzR9=\n", null},
        };
        for (Object[] testCase : cases) {
            assertEquals(testCase[1], GoText.readJwtExpirySeconds("e30." + testCase[0] + ".signature"),
                    (String) testCase[0]);
        }
        final StringBuilder claims = new StringBuilder("{\"exp\":1234,\"nested\":");
        for (int index = 0; index < 600; index++) {
            claims.append('[');
        }
        claims.append('0');
        for (int index = 0; index < 600; index++) {
            claims.append(']');
        }
        claims.append('}');
        final String payload = Base64.getUrlEncoder().withoutPadding()
                .encodeToString(claims.toString().getBytes(StandardCharsets.UTF_8));
        assertNull(GoText.readJwtExpirySeconds("e30." + payload + ".signature"));
    }

    @Test
    void parsesGoDurationsAndMailAddresses() {
        assertEquals(Duration.ofMinutes(90), GoDurations.parse("1h30m"));
        assertEquals(Duration.ofNanos(1500), GoDurations.parse("1.5µs"));
        assertEquals(Duration.ofMillis(-250), GoDurations.parse("-.25s"));
        assertThrows(IllegalArgumentException.class, () -> GoDurations.parse("1d"));
        assertThrows(IllegalArgumentException.class, () -> GoDurations.parse("3000000h"));
        assertTrue(GoText.isGoMailAddress("user.name@example.com"));
        assertTrue(GoText.isGoMailAddress("user@[192.0.2.1]"));
        assertFalse(GoText.isGoMailAddress("user@[fe80::1%eth0]"));
        assertFalse(GoText.isGoMailAddress("a..b@example.com"));
        assertFalse(GoText.isGoMailAddress("a@b@example.com"));
        assertEquals("value", GoText.trimSpace("　\t value  "));
    }

    @Test
    void jwtBearerSignsSortedAsciiClaimsAndParsesPkcs1() throws Exception {
        final KeyPairGenerator generator = KeyPairGenerator.getInstance("RSA");
        generator.initialize(2048);
        final KeyPair keyPair = generator.generateKeyPair();
        final String pkcs8 = "-----BEGIN PRIVATE KEY-----\n"
                + Base64.getMimeEncoder().encodeToString(keyPair.getPrivate().getEncoded()) + "\n-----END PRIVATE KEY-----\n";
        final RSAPrivateCrtKey parsed = (RSAPrivateCrtKey) JwtBearerAssertion.parseRsaPrivateKeyPem(pkcs8);
        final SecretString signed = new JwtBearerAssertion("svc@example.iam", "https://oauth2.example/token",
                Instant.ofEpochSecond(1_700_000_000L), Duration.ofHours(1), "usér@example.com", "a b")
                .sign(parsed);
        final String[] segments = signed.reveal().split("\\.");
        assertEquals("{\"alg\":\"RS256\",\"typ\":\"JWT\"}",
                new String(Base64.getUrlDecoder().decode(segments[0]), StandardCharsets.UTF_8));
        assertEquals("{\"aud\":\"https://oauth2.example/token\",\"exp\":1700003600,\"iat\":1700000000,"
                + "\"iss\":\"svc@example.iam\",\"scope\":\"a b\",\"sub\":\"us\\u00e9r@example.com\"}",
                new String(Base64.getUrlDecoder().decode(segments[1]), StandardCharsets.UTF_8));
        final Signature verifier = Signature.getInstance("SHA256withRSA");
        verifier.initVerify(keyPair.getPublic());
        verifier.update((segments[0] + "." + segments[1]).getBytes(StandardCharsets.US_ASCII));
        assertTrue(verifier.verify(Base64.getUrlDecoder().decode(segments[2])));
        assertThrows(IllegalArgumentException.class, () -> JwtBearerAssertion.parseRsaPrivateKeyPem("not a key"));
        final byte[] pkcs1 = pkcs1From(keyPair.getPrivate().getEncoded());
        final String pkcs1Pem = "-----BEGIN RSA PRIVATE KEY-----\n" + Base64.getMimeEncoder().encodeToString(pkcs1)
                + "\n-----END RSA PRIVATE KEY-----\n";
        assertEquals(parsed.getModulus(),
                ((RSAPrivateCrtKey) JwtBearerAssertion.parseRsaPrivateKeyPem(pkcs1Pem)).getModulus());
    }

    @Test
    void sharedRefreshRulesMatchPython() {
        final Instant issued = Instant.parse("2026-01-01T00:00:00Z");
        assertEquals(Duration.ofMinutes(5), OAuthRefreshRules.proportionalRefreshSkew(null, null));
        assertEquals(Duration.ofMinutes(2), OAuthRefreshRules.proportionalRefreshSkew(issued, issued.plus(Duration.ofMinutes(8))));
        assertEquals(Duration.ofSeconds(30), OAuthRefreshRules.forcedRefreshFloor(issued, issued.plus(Duration.ofMinutes(2))));
        assertTrue(OAuthRefreshRules.isRefreshRequired(false, null, issued, MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING, null));
        assertFalse(OAuthRefreshRules.isRefreshRequired(true, null, issued, MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING, null));
        assertTrue(OAuthRefreshRules.isRefreshRequired(true, issued.plus(Duration.ofMinutes(5)), issued,
                MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING, null));
        assertTrue(OAuthRefreshRules.hasAllScopes("a,b c", Arrays.asList("a", "c")));
        assertFalse(OAuthRefreshRules.hasExactScopes("a,b c", Arrays.asList("a", "c")));
    }

    private static byte[] pkcs1From(final byte[] pkcs8) {
        // PKCS #8 wraps the PKCS #1 key in a final OCTET STRING; find it by its length prefix.
        int index = 0;
        index += 1;
        index += lengthBytes(pkcs8, index);
        index += 3;
        index += 2 + (pkcs8[index + 1] & 0xff);
        index += 1;
        final int contentLength = readLength(pkcs8, index);
        index += lengthBytes(pkcs8, index);
        return Arrays.copyOfRange(pkcs8, index, index + contentLength);
    }

    private static int lengthBytes(final byte[] der, final int offset) {
        final int first = der[offset] & 0xff;
        return first < 0x80 ? 1 : 1 + (first & 0x7f);
    }

    private static int readLength(final byte[] der, final int offset) {
        final int first = der[offset] & 0xff;
        if (first < 0x80) {
            return first;
        }
        int length = 0;
        for (int index = 1; index <= (first & 0x7f); index++) {
            length = (length << 8) | (der[offset + index] & 0xff);
        }
        return length;
    }

    private static ManifestCredentialRefreshDriver<MapCredentials> driver(final JsonNode auth, final RecordingTransport transport) {
        return new ManifestCredentialRefreshDriver<>("Example", auth, MapCredentials.accessor(FIELDS), transport);
    }

    private static RecordingTransport respond(final String body) {
        final RecordingTransport transport = new RecordingTransport();
        transport.body = body.getBytes(StandardCharsets.UTF_8);
        return transport;
    }

    private static ObjectNode auth() {
        try {
            return (ObjectNode) MAPPER.readTree(AUTH);
        } catch (java.io.IOException error) {
            throw new IllegalStateException(error);
        }
    }

    private static ObjectNode oauth(final ObjectNode auth) {
        return (ObjectNode) ((ArrayNode) auth.get("methods")).get(0).get("oauth2");
    }

    private static MapCredentials credentials() {
        return credentials("access_token", "old");
    }

    private static MapCredentials credentials(final String field, final String value) {
        final Map<String, String> values = new LinkedHashMap<>();
        values.put("access_token", "old");
        values.put("refresh_token", "prior");
        values.put("client_id", "client-id");
        values.put("client_secret", "client-secret");
        values.put("instance_url", "old-instance");
        values.put(field, value);
        return new MapCredentials(values, "oauth");
    }

    private static CredentialRefreshDriver.State<MapCredentials> state(final MapCredentials credentials, final Instant expiresAt) {
        return new CredentialRefreshDriver.State<>(credentials, expiresAt, Instant.now());
    }
}
