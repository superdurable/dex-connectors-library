// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.relaysignature;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.AbstractMap.SimpleImmutableEntry;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import org.junit.jupiter.api.Test;

class RelaySignaturesTest {
    private static final byte[] BODY = "{\"event_id\":\"Ev1\"}".getBytes(StandardCharsets.UTF_8);
    private static final Instant SIGNED_AT = Instant.ofEpochSecond(1_760_227_200L);

    @Test
    void signsTheSharedVectorAndReturnsTypedRefusals() {
        final String signature = RelaySignatures.signRelayedRequest("scope-token", SIGNED_AT, BODY);
        assertEquals("v1=b94d74f33919d2a8390208d6274f78e61574baa906f03f74d58465e2c4e96bf7", signature);
        final List<Map.Entry<String, String>> headers = headers("scope-token", SIGNED_AT);
        final RelaySignatureVerification valid = RelaySignatures.verifyRelaySignature(
                "scope-token", headers, BODY, SIGNED_AT.plus(Duration.ofMinutes(1)));
        assertTrue(valid.isValid());
        assertEquals(SIGNED_AT, valid.getSignedAt());
        assertTrue(RelaySignatures.verifyRelaySignature("scope-token", pairs(
                RelaySignatures.RELAY_TIMESTAMP_HEADER, "1760227200", RelaySignatures.RELAY_SIGNATURE_HEADER, signature),
                BODY, SIGNED_AT).isValid());
        assertEquals(RelaySignatureRefusal.MISMATCH,
                RelaySignatures.verifyRelaySignature("other-token", headers, BODY, SIGNED_AT).getRefusal());
        assertEquals(RelaySignatureRefusal.MISMATCH, RelaySignatures.verifyRelaySignature("scope-token", headers,
                "{\"event_id\":\"Ev2\"}".getBytes(StandardCharsets.UTF_8), SIGNED_AT).getRefusal());
        assertEquals(RelaySignatureRefusal.MISSING, RelaySignatures.verifyRelaySignature("scope-token",
                pairs(RelaySignatures.RELAY_TIMESTAMP_HEADER, "1760227200"), BODY, SIGNED_AT).getRefusal());
        assertEquals(RelaySignatureRefusal.STALE, RelaySignatures.verifyRelaySignature("scope-token", headers, BODY,
                SIGNED_AT.plus(Duration.ofMinutes(6))).getRefusal());
        assertNull(RelaySignatures.verifyRelaySignature("other-token", headers, BODY, SIGNED_AT).getSignedAt());
    }

    @Test
    void refusesMalformedAndOutOfRangeTimestampsWithoutErrors() {
        final String signature = RelaySignatures.signRelayedRequest("scope-token", SIGNED_AT, BODY);
        final String[][] malformed = {
            {"+1760227200", signature},
            {"1111111111111111", signature},
            {"1760227200\n", signature},
            {"1760227200", "v2=" + repeat('0', 64)},
            {"1760227200", "v1=" + repeat('0', 63)},
            {"1760227200", "v1=" + repeat('g', 64)},
        };
        for (String[] pair : malformed) {
            assertEquals(RelaySignatureRefusal.MALFORMED, RelaySignatures.parseRelaySignature(pairs(
                    RelaySignatures.RELAY_TIMESTAMP_HEADER, pair[0], RelaySignatures.RELAY_SIGNATURE_HEADER, pair[1]),
                    SIGNED_AT).getRefusal(), pair[0] + " " + pair[1]);
        }
        for (String timestamp : new String[] {"999999999999999", "-999999999999999"}) {
            assertEquals(RelaySignatureRefusal.STALE, RelaySignatures.parseRelaySignature(pairs(
                    RelaySignatures.RELAY_TIMESTAMP_HEADER, timestamp, RelaySignatures.RELAY_SIGNATURE_HEADER, signature),
                    SIGNED_AT).getRefusal());
        }
        final long[][] offsets = {{300, 1}, {-300, 1}, {301, 0}, {-301, 0}};
        for (long[] offset : offsets) {
            final ParsedRelaySignature parsed = RelaySignatures.parseRelaySignature(
                    headers("t", SIGNED_AT), SIGNED_AT.plusSeconds(offset[0]));
            assertEquals(offset[1] == 1, parsed.getRefusal() == null, Long.toString(offset[0]));
        }
        final String upperHex = "v1=" + signature.substring(3).toUpperCase(Locale.ROOT);
        assertTrue(RelaySignatures.verifyRelaySignature("scope-token", pairs(
                RelaySignatures.RELAY_TIMESTAMP_HEADER, "1760227200", RelaySignatures.RELAY_SIGNATURE_HEADER, upperHex),
                BODY, SIGNED_AT).isValid());
    }

    private static List<Map.Entry<String, String>> headers(final String token, final Instant signedAt) {
        return pairs(RelaySignatures.RELAY_TIMESTAMP_HEADER.toLowerCase(Locale.ROOT),
                Long.toString(signedAt.getEpochSecond()),
                RelaySignatures.RELAY_SIGNATURE_HEADER.toLowerCase(Locale.ROOT),
                RelaySignatures.signRelayedRequest(token, signedAt, BODY));
    }

    private static List<Map.Entry<String, String>> pairs(final String... namesAndValues) {
        final List<Map.Entry<String, String>> entries = new ArrayList<>();
        for (int index = 0; index + 1 < namesAndValues.length; index += 2) {
            entries.add(new SimpleImmutableEntry<>(namesAndValues[index], namesAndValues[index + 1]));
        }
        return entries;
    }

    private static String repeat(final char character, final int count) {
        final char[] characters = new char[count];
        Arrays.fill(characters, character);
        return new String(characters);
    }
}
