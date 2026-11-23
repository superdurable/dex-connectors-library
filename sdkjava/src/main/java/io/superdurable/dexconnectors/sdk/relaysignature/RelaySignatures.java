// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.relaysignature;

import java.nio.charset.StandardCharsets;
import java.security.InvalidKeyException;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Duration;
import java.time.Instant;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Pattern;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * Signs and verifies requests a Dex host relays to an application with its scope token.
 *
 * <p>The scheme matches the Go, Python, and TypeScript Connector SDKs: {@code Dex-Relay-Timestamp}
 * carries the Unix second of signing and {@code Dex-Relay-Signature} carries {@code v1=} and the
 * hexadecimal HMAC-SHA256 of {@code v1:<timestamp>:<raw body>} keyed with the scope token. A
 * request is valid only when the signature matches in constant time and the timestamp is within
 * five minutes of receipt. These methods never log, store, or return the token.
 *
 * <p>Headers are name and value pairs; names match case-insensitively and the first value of a
 * name wins.
 */
public final class RelaySignatures {
    /** Header carrying the Unix second at which the host signed a relayed request. */
    public static final String RELAY_TIMESTAMP_HEADER = "Dex-Relay-Timestamp";
    /** Header carrying {@code v1=} and the hexadecimal HMAC-SHA256 of {@code v1:<timestamp>:<body>}. */
    public static final String RELAY_SIGNATURE_HEADER = "Dex-Relay-Signature";
    /** How far a relayed request's timestamp may be from the receiver's clock; the boundary is valid. */
    public static final Duration RELAY_TIMESTAMP_TOLERANCE = Duration.ofMinutes(5);

    private static final String VERSION = "v1";
    private static final Pattern TIMESTAMP = Pattern.compile("^-?[0-9]{1,15}$");
    private static final Pattern SIGNATURE_HEX = Pattern.compile("^[0-9a-fA-F]{64}$");

    private RelaySignatures() {
    }

    /**
     * Returns the {@code Dex-Relay-Signature} value for a body signed at the Unix second of
     * {@code signedAt}; send the same second in {@code Dex-Relay-Timestamp}.
     *
     * @param token the scope token
     * @param signedAt the signing time
     * @param body the raw body
     * @return the signature header value
     */
    public static String signRelayedRequest(final String token, final Instant signedAt, final byte[] body) {
        return VERSION + "=" + hex(relaySignature(token, Long.toString(signedAt.getEpochSecond()), body));
    }

    /**
     * Parses the relay headers and checks the timestamp window without the token.
     *
     * <p>A receiver can refuse an unsigned, malformed, or stale request before reading any secret.
     *
     * @param headers the request header pairs
     * @param receivedAt when the request was received
     * @return the parsed signature, or one carrying the refusal
     */
    public static ParsedRelaySignature parseRelaySignature(
            final Iterable<? extends Map.Entry<String, String>> headers, final Instant receivedAt) {
        final String timestamp = header(headers, RELAY_TIMESTAMP_HEADER);
        final String signatureHeader = header(headers, RELAY_SIGNATURE_HEADER);
        if (timestamp.isEmpty() || signatureHeader.isEmpty()) {
            return ParsedRelaySignature.refused(RelaySignatureRefusal.MISSING);
        }
        if (!TIMESTAMP.matcher(timestamp).matches() || !signatureHeader.startsWith(VERSION + "=")) {
            return ParsedRelaySignature.refused(RelaySignatureRefusal.MALFORMED);
        }
        final String encoded = signatureHeader.substring(VERSION.length() + 1);
        if (!SIGNATURE_HEX.matcher(encoded).matches()) {
            return ParsedRelaySignature.refused(RelaySignatureRefusal.MALFORMED);
        }
        final long seconds = Long.parseLong(timestamp);
        final long receivedMicros = Math.addExact(Math.multiplyExact(receivedAt.getEpochSecond(), 1_000_000L),
                receivedAt.getNano() / 1_000L);
        if (Math.abs(receivedMicros - seconds * 1_000_000L) > RELAY_TIMESTAMP_TOLERANCE.toNanos() / 1_000L) {
            return ParsedRelaySignature.refused(RelaySignatureRefusal.STALE);
        }
        return ParsedRelaySignature.parsed(timestamp, Instant.ofEpochSecond(seconds), unhex(encoded));
    }

    /**
     * Reports whether a parsed signature matches the token over the raw body, in constant time.
     *
     * @param parsed the parsed signature without a refusal
     * @param token the scope token
     * @param body the raw body
     * @return {@code true} for a match
     */
    public static boolean matchesRelaySignature(final ParsedRelaySignature parsed, final String token, final byte[] body) {
        if (parsed.getRefusal() != null) {
            return false;
        }
        return MessageDigest.isEqual(parsed.signature(), relaySignature(token, parsed.getTimestamp(), body));
    }

    /**
     * Verifies one relayed request's headers and raw body against the token at receipt time.
     *
     * @param token the scope token
     * @param headers the request header pairs
     * @param body the raw body
     * @param receivedAt when the request was received
     * @return the verification outcome
     */
    public static RelaySignatureVerification verifyRelaySignature(
            final String token, final Iterable<? extends Map.Entry<String, String>> headers, final byte[] body,
            final Instant receivedAt) {
        final ParsedRelaySignature parsed = parseRelaySignature(headers, receivedAt);
        if (parsed.getRefusal() != null) {
            return new RelaySignatureVerification(null, parsed.getRefusal());
        }
        if (!matchesRelaySignature(parsed, token, body)) {
            return new RelaySignatureVerification(null, RelaySignatureRefusal.MISMATCH);
        }
        return new RelaySignatureVerification(parsed.getSignedAt(), null);
    }

    private static String header(final Iterable<? extends Map.Entry<String, String>> headers, final String name) {
        final String wanted = name.toLowerCase(Locale.ROOT);
        for (Map.Entry<String, String> entry : headers) {
            if (entry.getKey() != null && entry.getKey().toLowerCase(Locale.ROOT).equals(wanted)) {
                return entry.getValue() == null ? "" : entry.getValue();
            }
        }
        return "";
    }

    private static byte[] relaySignature(final String token, final String timestamp, final byte[] body) {
        try {
            final Mac mac = Mac.getInstance("HmacSHA256");
            final byte[] key = token.getBytes(StandardCharsets.UTF_8);
            // An empty HMAC key is valid, but SecretKeySpec rejects it; a single zero byte is equivalent.
            mac.init(new SecretKeySpec(key.length == 0 ? new byte[1] : key, "HmacSHA256"));
            mac.update((VERSION + ":" + timestamp + ":").getBytes(StandardCharsets.UTF_8));
            mac.update(body);
            return mac.doFinal();
        } catch (NoSuchAlgorithmException | InvalidKeyException error) {
            throw new IllegalStateException("HmacSHA256 is required by every Java platform", error);
        }
    }

    private static String hex(final byte[] bytes) {
        final StringBuilder builder = new StringBuilder(bytes.length * 2);
        for (byte value : bytes) {
            builder.append(Character.forDigit((value >> 4) & 0xf, 16)).append(Character.forDigit(value & 0xf, 16));
        }
        return builder.toString();
    }

    private static byte[] unhex(final String text) {
        final byte[] bytes = new byte[text.length() / 2];
        for (int index = 0; index < bytes.length; index++) {
            bytes[index] = (byte) Integer.parseInt(text.substring(index * 2, index * 2 + 2), 16);
        }
        return bytes;
    }
}
