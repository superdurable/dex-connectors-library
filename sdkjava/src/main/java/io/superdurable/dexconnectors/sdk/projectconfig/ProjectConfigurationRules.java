// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.net.InetAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.UnknownHostException;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Set;
import java.util.regex.Pattern;

/**
 * Validation rules shared by project configuration documents, secrets, and the broker endpoint.
 */
public final class ProjectConfigurationRules {
    /** Reserved secret object name of the credential broker scope token. */
    public static final String CREDENTIAL_BROKER_SECRET_NAME = "DEX_CREDENTIAL_BROKER_TOKEN";

    static final Pattern DIGEST = Pattern.compile("^sha256:[0-9a-f]{64}$");
    static final Pattern CONNECTOR_ID = Pattern.compile("^[a-z][a-z0-9-]{1,62}$");

    private static final Pattern ENVIRONMENT_NAME = Pattern.compile("^[A-Z][A-Z0-9_]{0,127}$");
    private static final Pattern SECRET_ID = Pattern.compile("^[0-9a-f]{32}$");
    private static final Set<String> RESERVED_NAMES = new HashSet<>(Arrays.asList(
            "PATH", "HOME", "PORT", "HOST", "NODE_OPTIONS", "SSL_CERT_FILE", "SSL_CERT_DIR", "PUBLIC_BASE_URL",
            "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"));
    private static final String[] RESERVED_PREFIXES = {"SUPERVERSE_", "DEX_", "AWS_", "LD_", "GO", "GIT_"};
    private static final int MAXIMUM_ENVIRONMENT_VALUE_BYTES = 32768;

    private ProjectConfigurationRules() {
    }

    /**
     * Reports whether a name is a valid application environment name.
     *
     * <p>Names reserved for deployment, SDK, trust, and proxy settings are rejected.
     *
     * @param name the environment name
     * @return {@code true} when the application may use the name
     */
    public static boolean isApplicationEnvironmentName(final String name) {
        if (name == null || !ENVIRONMENT_NAME.matcher(name).matches() || RESERVED_NAMES.contains(name)) {
            return false;
        }
        for (String prefix : RESERVED_PREFIXES) {
            if (name.startsWith(prefix)) {
                return false;
            }
        }
        return true;
    }

    /**
     * Limits a startup value to 32768 UTF-8 bytes without NUL characters.
     *
     * @param value the environment value
     * @throws IllegalArgumentException if the value is invalid
     */
    public static void validateEnvironmentValue(final String value) {
        if (value == null || value.indexOf('\u0000') >= 0
                || value.getBytes(StandardCharsets.UTF_8).length > MAXIMUM_ENVIRONMENT_VALUE_BYTES) {
            throw new IllegalArgumentException("invalid application environment value");
        }
    }

    /**
     * Requires an exact secret object beneath this scope and field name.
     *
     * @param reference the secret reference
     * @param scope the project scope
     * @param name the environment or reserved secret name
     * @throws IllegalArgumentException if the reference differs from the scope
     */
    public static void validateSecretReference(
            final ApplicationSecretRef reference, final Scope scope, final String name) {
        final String prefix = scope.prefix() + "/app-secrets/" + name + "/";
        if (!reference.getKey().startsWith(prefix)
                || !SECRET_ID.matcher(reference.getKey().substring(prefix.length())).matches()
                || reference.getVersion().isEmpty()
                || "null".equals(reference.getVersion())
                || !DIGEST.matcher(reference.getDigest()).matches()) {
            throw new IllegalArgumentException("application secret reference differs from scope");
        }
    }

    /**
     * Requires a bounded absolute broker endpoint; only private hosts may use HTTP.
     *
     * @param value the endpoint URL
     * @throws IllegalArgumentException if the endpoint is invalid
     */
    public static void validateBrokerEndpoint(final String value) {
        final URI parsed = parseUri(value);
        if (value.length() > 2048 || parsed == null
                || !("http".equals(parsed.getScheme()) || "https".equals(parsed.getScheme()))
                || parsed.getHost() == null || parsed.getHost().isEmpty()
                || parsed.getRawUserInfo() != null || parsed.getRawQuery() != null || parsed.getRawFragment() != null) {
            throw new IllegalArgumentException("credential broker endpoint must be an absolute HTTP or HTTPS URL");
        }
        if ("https".equals(parsed.getScheme())) {
            return;
        }
        final String host = hostWithoutBrackets(parsed.getHost());
        boolean isPrivate = "localhost".equals(host) || "host.docker.internal".equals(host)
                || !host.contains(".") || host.endsWith(".svc") || host.endsWith(".svc.cluster.local");
        final InetAddress address = literalAddress(host);
        if (address != null) {
            isPrivate = isPrivate || isPrivateAddress(address);
        }
        if (!isPrivate) {
            throw new IllegalArgumentException("credential broker HTTP endpoint must use a private host");
        }
    }

    static boolean isLocalStorageEndpoint(final String value) {
        final URI parsed = parseUri(value);
        if (parsed == null || !("http".equals(parsed.getScheme()) || "https".equals(parsed.getScheme()))
                || parsed.getHost() == null || parsed.getHost().isEmpty() || parsed.getRawUserInfo() != null
                || parsed.getRawQuery() != null || parsed.getRawFragment() != null
                || !(parsed.getRawPath() == null || parsed.getRawPath().isEmpty() || "/".equals(parsed.getRawPath()))) {
            return false;
        }
        final String host = hostWithoutBrackets(parsed.getHost());
        if ("localhost".equals(host) || "host.docker.internal".equals(host) || host.endsWith(".svc.cluster.local")) {
            return true;
        }
        final InetAddress address = literalAddress(host);
        return address != null && (address.isLoopbackAddress() || isPrivateAddress(address));
    }

    static void requireFields(final ObjectNode record, final String label, final String... allowed) {
        final Set<String> allowedNames = new HashSet<>(Arrays.asList(allowed));
        final Iterator<String> names = record.fieldNames();
        while (names.hasNext()) {
            if (!allowedNames.contains(names.next())) {
                throw new IllegalArgumentException(label + " has unknown fields");
            }
        }
    }

    static String text(final JsonNode record, final String name) {
        final JsonNode value = record.get(name);
        if (value == null || value.isNull()) {
            return "";
        }
        return value.isTextual() ? value.textValue() : value.toString();
    }

    private static URI parseUri(final String value) {
        if (value == null) {
            return null;
        }
        try {
            return new URI(value);
        } catch (URISyntaxException error) {
            return null;
        }
    }

    private static String hostWithoutBrackets(final String host) {
        return host.startsWith("[") && host.endsWith("]") ? host.substring(1, host.length() - 1) : host;
    }

    private static InetAddress literalAddress(final String host) {
        final boolean isIpv4 = host.matches("^[0-9]{1,3}(\\.[0-9]{1,3}){3}$");
        if (!isIpv4 && !host.contains(":")) {
            return null;
        }
        try {
            return InetAddress.getByName(host);
        } catch (UnknownHostException error) {
            return null;
        }
    }

    private static boolean isPrivateAddress(final InetAddress address) {
        if (address.isLoopbackAddress() || address.isLinkLocalAddress() || address.isSiteLocalAddress()) {
            return true;
        }
        final byte[] bytes = address.getAddress();
        // IPv6 unique local addresses fc00::/7 are private, as Python's ipaddress reports them.
        return bytes.length == 16 && (bytes[0] & 0xfe) == 0xfc;
    }
}
