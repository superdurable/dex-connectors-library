// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import io.superdurable.dexconnectors.sdk.SecretString;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.KeyFactory;
import java.security.PrivateKey;
import java.security.Signature;
import java.security.interfaces.RSAPrivateKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.time.Duration;
import java.time.Instant;
import java.util.Base64;
import java.util.Map;
import java.util.TreeMap;

/**
 * RFC 7523 RS256 assertion claims for a service-account grant.
 *
 * <p>{@link #sign(PrivateKey)} produces the compact JWT that
 * {@link TokenEndpoint#exchangeJwtBearerAssertion} sends. Header and claims are encoded as compact
 * JSON with sorted keys and ASCII escapes, byte-identical to the Python SDK.
 */
public final class JwtBearerAssertion {
    private static final byte[] RSA_ENCRYPTION_ALGORITHM = {
        0x30, 0x0d, 0x06, 0x09, 0x2a, (byte) 0x86, 0x48, (byte) 0x86, (byte) 0xf7, 0x0d, 0x01, 0x01, 0x01, 0x05, 0x00,
    };

    private final String issuer;
    private final String audience;
    private final Instant issuedAt;
    private final Duration lifetime;
    private final String subject;
    private final String scope;

    /**
     * Creates assertion claims.
     *
     * @param issuer the {@code iss} claim
     * @param audience the {@code aud} claim
     * @param issuedAt the {@code iat} time
     * @param lifetime the positive lifetime that sets {@code exp}
     * @param subject the {@code sub} claim, or empty
     * @param scope the space-separated {@code scope} claim, or empty
     */
    public JwtBearerAssertion(
            final String issuer, final String audience, final Instant issuedAt, final Duration lifetime,
            final String subject, final String scope) {
        this.issuer = issuer == null ? "" : issuer;
        this.audience = audience == null ? "" : audience;
        this.issuedAt = issuedAt;
        this.lifetime = lifetime;
        this.subject = subject == null ? "" : subject;
        this.scope = scope == null ? "" : scope;
    }

    /**
     * Parses an RSA PKCS #1 or PKCS #8 PEM private key without echoing key material.
     *
     * @param pemText the unencrypted PEM text
     * @return the RSA private key
     * @throws IllegalArgumentException if the key is invalid or not RSA
     */
    public static RSAPrivateKey parseRsaPrivateKeyPem(final String pemText) {
        final boolean isPkcs1 = pemText.contains("-----BEGIN RSA PRIVATE KEY-----");
        final String label = isPkcs1 ? "RSA PRIVATE KEY" : "PRIVATE KEY";
        final int start = pemText.indexOf("-----BEGIN " + label + "-----");
        final int end = pemText.indexOf("-----END " + label + "-----");
        if (start < 0 || end < start) {
            throw new IllegalArgumentException("private key is invalid");
        }
        final byte[] der;
        try {
            der = Base64.getMimeDecoder().decode(
                    pemText.substring(start + ("-----BEGIN " + label + "-----").length(), end).trim());
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("private key is invalid");
        }
        final byte[] pkcs8 = isPkcs1 ? wrapPkcs1(der) : der;
        try {
            final PrivateKey key = KeyFactory.getInstance("RSA").generatePrivate(new PKCS8EncodedKeySpec(pkcs8));
            if (!(key instanceof RSAPrivateKey)) {
                throw new IllegalArgumentException("private key must use RSA");
            }
            return (RSAPrivateKey) key;
        } catch (GeneralSecurityException | RuntimeException error) {
            if (error instanceof IllegalArgumentException
                    && "private key must use RSA".equals(error.getMessage())) {
                throw (IllegalArgumentException) error;
            }
            throw new IllegalArgumentException("private key is invalid");
        }
    }

    /**
     * Signs a compact RS256 assertion.
     *
     * @param privateKey the RSA private key
     * @return the compact JWT
     * @throws IllegalArgumentException if the claims are incomplete or signing fails
     */
    public SecretString sign(final PrivateKey privateKey) {
        if (issuer.isEmpty() || audience.isEmpty() || lifetime == null || lifetime.isZero() || lifetime.isNegative()
                || issuedAt == null) {
            throw new IllegalArgumentException("JWT-bearer assertion claims are incomplete");
        }
        final Map<String, Object> claims = new TreeMap<>();
        claims.put("aud", audience);
        claims.put("exp", floorSeconds(issuedAt.plus(lifetime)));
        claims.put("iat", floorSeconds(issuedAt));
        claims.put("iss", issuer);
        if (!subject.isEmpty()) {
            claims.put("sub", subject);
        }
        if (!scope.isEmpty()) {
            claims.put("scope", scope);
        }
        final Map<String, Object> header = new TreeMap<>();
        header.put("alg", "RS256");
        header.put("typ", "JWT");
        final Base64.Encoder encoder = Base64.getUrlEncoder().withoutPadding();
        final String unsigned = encoder.encodeToString(compactAsciiJson(header).getBytes(StandardCharsets.US_ASCII))
                + "." + encoder.encodeToString(compactAsciiJson(claims).getBytes(StandardCharsets.US_ASCII));
        try {
            final Signature signature = Signature.getInstance("SHA256withRSA");
            signature.initSign(privateKey);
            signature.update(unsigned.getBytes(StandardCharsets.US_ASCII));
            return new SecretString(unsigned + "." + encoder.encodeToString(signature.sign()));
        } catch (GeneralSecurityException error) {
            throw new IllegalArgumentException("private key is invalid");
        }
    }

    /**
     * Returns the issuer claim.
     *
     * @return the issuer
     */
    public String getIssuer() {
        return issuer;
    }

    /**
     * Returns the audience claim.
     *
     * @return the audience
     */
    public String getAudience() {
        return audience;
    }

    /**
     * Returns the issue time.
     *
     * @return the issue time
     */
    public Instant getIssuedAt() {
        return issuedAt;
    }

    /**
     * Returns the lifetime.
     *
     * @return the lifetime
     */
    public Duration getLifetime() {
        return lifetime;
    }

    /**
     * Returns the subject claim.
     *
     * @return the subject, or empty
     */
    public String getSubject() {
        return subject;
    }

    /**
     * Returns the scope claim.
     *
     * @return the scope, or empty
     */
    public String getScope() {
        return scope;
    }

    private static long floorSeconds(final Instant value) {
        return value.getEpochSecond();
    }

    private static String compactAsciiJson(final Map<String, Object> object) {
        final StringBuilder json = new StringBuilder("{");
        boolean isFirst = true;
        for (Map.Entry<String, Object> entry : object.entrySet()) {
            if (!isFirst) {
                json.append(',');
            }
            isFirst = false;
            appendJsonString(json, entry.getKey());
            json.append(':');
            if (entry.getValue() instanceof String) {
                appendJsonString(json, (String) entry.getValue());
            } else {
                json.append(entry.getValue());
            }
        }
        return json.append('}').toString();
    }

    private static void appendJsonString(final StringBuilder json, final String value) {
        json.append('"');
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            switch (character) {
                case '"':
                    json.append("\\\"");
                    break;
                case '\\':
                    json.append("\\\\");
                    break;
                case '\n':
                    json.append("\\n");
                    break;
                case '\r':
                    json.append("\\r");
                    break;
                case '\t':
                    json.append("\\t");
                    break;
                case '\b':
                    json.append("\\b");
                    break;
                case '\f':
                    json.append("\\f");
                    break;
                default:
                    if (character < 0x20 || character > 0x7e) {
                        json.append(String.format(java.util.Locale.ROOT, "\\u%04x", (int) character));
                    } else {
                        json.append(character);
                    }
            }
        }
        json.append('"');
    }

    private static byte[] wrapPkcs1(final byte[] pkcs1) {
        final ByteArrayOutputStream body = new ByteArrayOutputStream();
        body.write(new byte[] {0x02, 0x01, 0x00}, 0, 3);
        body.write(RSA_ENCRYPTION_ALGORITHM, 0, RSA_ENCRYPTION_ALGORITHM.length);
        writeDer(body, 0x04, pkcs1);
        final ByteArrayOutputStream sequence = new ByteArrayOutputStream();
        writeDer(sequence, 0x30, body.toByteArray());
        return sequence.toByteArray();
    }

    private static void writeDer(final ByteArrayOutputStream output, final int tag, final byte[] contents) {
        output.write(tag);
        final int length = contents.length;
        if (length < 0x80) {
            output.write(length);
        } else if (length < 0x100) {
            output.write(0x81);
            output.write(length);
        } else if (length < 0x10000) {
            output.write(0x82);
            output.write(length >>> 8);
            output.write(length);
        } else {
            output.write(0x83);
            output.write(length >>> 16);
            output.write(length >>> 8);
            output.write(length);
        }
        output.write(contents, 0, contents.length);
    }
}
