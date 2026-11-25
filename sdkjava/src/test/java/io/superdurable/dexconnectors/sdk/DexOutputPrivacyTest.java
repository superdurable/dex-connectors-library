// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dex.WorkerOptions;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpRequest;
import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import org.junit.jupiter.api.Test;

/** Private request and refresh values never reach Dex output, logs, or plain Jackson JSON. */
class DexOutputPrivacyTest {
    private static final String TOKEN = "synthetic-refresh-token-4f1c";

    @Test
    void privateTransportValuesRefuseEveryJacksonPathIncludingDexOutput() throws Exception {
        final ObjectNode credential = JsonNodeFactory.instance.objectNode().put("refresh_token", TOKEN);
        final ProviderHttpRequest request = ProviderHttpRequest.newBuilder("POST", "https://provider.test/token")
                .header("Authorization", "Bearer " + TOKEN)
                .body(("refresh_token=" + TOKEN).getBytes(StandardCharsets.UTF_8))
                .build();
        final List<Object> values = Arrays.<Object>asList(
                request,
                new CredentialRefreshDriver.State<ObjectNode>(credential, Instant.EPOCH, Instant.EPOCH),
                new CredentialRefreshDriver.Result<ObjectNode>(credential, Instant.EPOCH));
        final Object dexOutputMapper = dexValueMapper();
        final Method encode = dexOutputMapper.getClass().getDeclaredMethod("encode", Object.class);
        encode.setAccessible(true);
        for (Object value : values) {
            for (Object wrapped : Arrays.asList(value, Collections.singletonMap("nested", value),
                    Collections.singletonList(value))) {
                for (ObjectMapper mapper : Arrays.asList(new ObjectMapper(), new ObjectMapper().findAndRegisterModules())) {
                    assertNoSecret(assertThrows(Exception.class, () -> mapper.writeValueAsString(wrapped)));
                }
                final InvocationTargetException dexError =
                        assertThrows(InvocationTargetException.class, () -> encode.invoke(dexOutputMapper, wrapped));
                assertNoSecret(dexError.getCause());
            }
            assertFalse(value.toString().contains(TOKEN), value.getClass().getName());
        }
    }

    // The Worker's output ValueMapper over its default ObjectMapper is the exact Step output path.
    private static Object dexValueMapper() throws Exception {
        final WorkerOptions options = WorkerOptions.newBuilder().build();
        final Method objectMapper = WorkerOptions.class.getDeclaredMethod("getObjectMapper");
        objectMapper.setAccessible(true);
        final Class<?> valueMapperClass = Class.forName("io.superdurable.dex.ValueMapper");
        final Constructor<?> constructor = valueMapperClass.getDeclaredConstructor(ObjectMapper.class);
        constructor.setAccessible(true);
        return constructor.newInstance(objectMapper.invoke(options));
    }

    private static void assertNoSecret(final Throwable error) {
        for (Throwable current = error; current != null; current = current.getCause()) {
            assertFalse(String.valueOf(current.getMessage()).contains(TOKEN), current.toString());
            assertFalse(current.toString().contains(TOKEN), current.toString());
        }
    }
}
