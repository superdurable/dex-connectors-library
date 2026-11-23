// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.oauthtoken.ClientAuthenticationMethod;
import io.superdurable.dexconnectors.sdk.oauthtoken.ClientCredentials;
import io.superdurable.dexconnectors.sdk.oauthtoken.TokenResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.attribute.PosixFilePermissions;
import java.time.Duration;
import java.time.Instant;
import java.time.OffsetDateTime;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import org.junit.jupiter.api.Test;

class PrivateValueSafetyTest {
    private static final String TOKEN = "REDACTED_SYNTHETIC_TOKEN_7f3a";

    @Test
    void privateValuesRefuseDefaultAndDexJacksonMappers() {
        final ObjectNode credentials = GoCompatibleJson.newObject().put("refresh_token", TOKEN);
        final List<Object> privateValues = Arrays.<Object>asList(
                new SecretString(TOKEN),
                new CredentialMaterial(credentials, Rfc3339NanoTimestamp.ZERO, "oauth2"),
                new BrokerCredential(credentials, 1, OffsetDateTime.now(), OffsetDateTime.now(), Instant.now()),
                new ProjectObject("projects/p/live/x", "1", "etag", TOKEN.getBytes(StandardCharsets.UTF_8)),
                new TokenResponse(new SecretString(TOKEN), new SecretString(TOKEN), Duration.ofHours(1), null, "",
                        "Bearer", Collections.<String, JsonNode>emptyMap(), Collections.<String, SecretString>emptyMap()),
                new ClientCredentials("client", new SecretString(TOKEN), ClientAuthenticationMethod.CLIENT_SECRET_POST));
        for (ObjectMapper mapper : Arrays.asList(new ObjectMapper(), new ObjectMapper().findAndRegisterModules())) {
            for (Object value : privateValues) {
                final Exception error = assertThrows(Exception.class, () -> mapper.writeValueAsString(value),
                        value.getClass().getSimpleName());
                assertChainHasNoSecret(error);
                final Exception treeError = assertThrows(Exception.class, () -> mapper.valueToTree(value));
                assertChainHasNoSecret(treeError);
                assertFalse(value.toString().contains(TOKEN), value.getClass().getSimpleName());
            }
            // A private value nested in an ordinary result is refused as a whole.
            assertChainHasNoSecret(assertThrows(Exception.class, () -> mapper.writeValueAsString(
                    Collections.singletonMap("result", new SecretString(TOKEN)))));
        }
    }

    @Test
    void credentialFileParseErrorsCarryNoCauseOrContents() throws Exception {
        final Path directory = Files.createTempDirectory("sdkjava-private-parse");
        final Path file = directory.resolve("connections.json");
        Files.write(file, ("{\"connections\":[{\"credentials\":{\"refresh_token\":" + TOKEN + "}}]}")
                .getBytes(StandardCharsets.UTF_8));
        Files.setPosixFilePermissions(file, PosixFilePermissions.fromString("rw-------"));
        final IllegalArgumentException error = assertThrows(IllegalArgumentException.class,
                () -> DexWebFiles.readPrivateFile(file));
        assertNull(error.getCause());
        assertChainHasNoSecret(error);
        final IllegalArgumentException parseError = assertThrows(IllegalArgumentException.class,
                () -> GoCompatibleJson.parse(("{\"access_token\":" + TOKEN).getBytes(StandardCharsets.UTF_8)));
        assertNull(parseError.getCause());
        assertEquals("project document is invalid JSON", parseError.getMessage());
        Files.delete(file);
        Files.delete(directory);
    }

    @Test
    void rawMessageFieldsKeepGoBytes() {
        final byte[] document = ("{\"schemaVersion\":\"s\",\"credentials\":{ \"z\" : 1e0, \"a\":-0.0,"
                + "\"html\":\"<&>\" },\"configuration\":{\"b\":[1.50,{\"y\":1,\"x\":2}],\"a\":null}}")
                .getBytes(StandardCharsets.UTF_8);
        final JsonNode parsed = GoCompatibleJson.parse(document);
        assertEquals("{\"schemaVersion\":\"s\",\"credentials\":{\"z\":1e0,\"a\":-0.0,\"html\":\"\\u003c\\u0026\\u003e\"},"
                + "\"configuration\":{\"b\":[1.50,{\"y\":1,\"x\":2}],\"a\":null}}",
                new String(GoCompatibleJson.encode(parsed), StandardCharsets.UTF_8));
        final ObjectNode copied = (ObjectNode) parsed.get("credentials").deepCopy();
        final ObjectNode envelope = GoCompatibleJson.newObject();
        envelope.set("credentials", copied);
        assertEquals("{\"credentials\":{\"z\":1e0,\"a\":-0.0,\"html\":\"\\u003c\\u0026\\u003e\"}}",
                new String(GoCompatibleJson.encode(envelope), StandardCharsets.UTF_8));
        copied.put("m", 2);
        assertTrue(new String(GoCompatibleJson.encode(envelope), StandardCharsets.UTF_8)
                .startsWith("{\"credentials\":{\"a\":"));
        final ObjectNode fresh = GoCompatibleJson.newObject();
        fresh.set("credentials", GoCompatibleJson.newObject().put("z", 1).put("a", 2));
        assertEquals("{\"credentials\":{\"a\":2,\"z\":1}}", new String(GoCompatibleJson.encode(fresh), StandardCharsets.UTF_8));
    }

    private static void assertChainHasNoSecret(final Throwable error) {
        Throwable current = error;
        while (current != null) {
            assertFalse(String.valueOf(current.getMessage()).contains(TOKEN), current.getClass().getName());
            assertFalse(String.valueOf(current).contains(TOKEN), current.getClass().getName());
            current = current.getCause();
        }
    }
}
