// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.core.util.DefaultIndenter;
import com.fasterxml.jackson.core.util.DefaultPrettyPrinter;
import com.fasterxml.jackson.core.util.Separators;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.attribute.BasicFileAttributes;
import java.nio.file.attribute.PosixFilePermission;
import java.nio.file.attribute.PosixFilePermissions;
import java.util.EnumSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;

/** Owner-only Dex Web file reads, decoding, and atomic writes. */
final class DexWebFiles {
    private static final Set<PosixFilePermission> OWNER_READ_WRITE =
            EnumSet.of(PosixFilePermission.OWNER_READ, PosixFilePermission.OWNER_WRITE);
    private static final ObjectMapper PRETTY_MAPPER = new ObjectMapper();

    private DexWebFiles() {
    }

    static ObjectNode readPrivateFile(final Path path) throws IOException {
        final BasicFileAttributes attributes =
                Files.readAttributes(path, BasicFileAttributes.class, LinkOption.NOFOLLOW_LINKS);
        if (!attributes.isRegularFile() || !hasOwnerOnlyMode(path)) {
            throw new IllegalArgumentException(path.getFileName() + " must be a regular 0600 file");
        }
        return GoCompatibleJson.parseObject(Files.readAllBytes(path), path.getFileName().toString());
    }

    static Map<ConnectionKey, CredentialMaterial> decodeLocalCredentials(final ObjectNode source) {
        if (!Configuration.LOCAL_CONNECTIONS_SCHEMA_VERSION.equals(ProjectConfigurationRules.text(source, "schemaVersion"))) {
            throw new IllegalArgumentException("unsupported connector configuration schema version");
        }
        final JsonNode connections = source.get("connections");
        if (connections == null || !connections.isArray()) {
            throw new IllegalArgumentException("connections.json requires connections");
        }
        final Map<ConnectionKey, CredentialMaterial> credentials = new LinkedHashMap<>();
        int index = 0;
        for (JsonNode raw : connections) {
            final ObjectNode record = GoCompatibleJson.requireObject(raw, "connection " + index++);
            final ConnectionKey key = new ConnectionKey(ProjectConfigurationRules.text(record, "connectorId"),
                    ProjectConfigurationRules.text(record, "connectionName"));
            key.path(new Scope("local", "preview"));
            if (credentials.containsKey(key)) {
                throw new IllegalArgumentException("duplicate local connection");
            }
            final ObjectNode material = GoCompatibleJson.requireObject(record.get("credentials"), "credentials");
            final JsonNode methods = record.get("authMethodIds");
            String authMethod = ProjectConfigurationRules.text(record, "authMethodId");
            if (authMethod.isEmpty()) {
                authMethod = methods != null && methods.isArray() && methods.size() == 1
                        ? (methods.get(0).isTextual() ? methods.get(0).textValue() : methods.get(0).toString())
                        : "local";
            }
            credentials.put(key, new CredentialMaterial(material, localExpiry(record.get("credentialExpiresAt")),
                    authMethod));
        }
        return credentials;
    }

    static Rfc3339NanoTimestamp localExpiry(final JsonNode value) {
        if (value == null || value.isNull()) {
            return null;
        }
        if (!value.isTextual()) {
            throw new IllegalArgumentException("local credential expiry is invalid");
        }
        return value.textValue().isEmpty() ? null : Rfc3339NanoTimestamp.fromWire(value.textValue());
    }

    static String credentialDigest(final CredentialMaterial material) {
        byte[] contents = GoCompatibleJson.encodeSortedMap(material.getCredentials());
        if (material.getExpiresAt() != null) {
            final byte[] expiry = ("|" + Rfc3339NanoTimestamp.fromInstant(material.getExpiresAtInstant()).toWire())
                    .getBytes(StandardCharsets.UTF_8);
            final byte[] combined = new byte[contents.length + expiry.length];
            System.arraycopy(contents, 0, combined, 0, contents.length);
            System.arraycopy(expiry, 0, combined, contents.length, expiry.length);
            contents = combined;
        }
        return GoCompatibleJson.sha256Hex(contents);
    }

    static byte[] prettyJson(final JsonNode value) throws IOException {
        final DefaultPrettyPrinter printer = new DefaultPrettyPrinter()
                .withSeparators(Separators.createDefaultInstance()
                        .withObjectFieldValueSpacing(Separators.Spacing.AFTER));
        printer.indentArraysWith(DefaultIndenter.SYSTEM_LINEFEED_INSTANCE.withLinefeed("\n"));
        printer.indentObjectsWith(DefaultIndenter.SYSTEM_LINEFEED_INSTANCE.withLinefeed("\n"));
        return (PRETTY_MAPPER.writer(printer).writeValueAsString(value) + "\n").getBytes(StandardCharsets.UTF_8);
    }

    static void writePrivateFile(final Path path, final byte[] contents) throws IOException {
        final Path directory = path.toAbsolutePath().getParent();
        final Path temporary = Files.createTempFile(directory, ".write-", ".tmp");
        try {
            setOwnerOnlyMode(temporary);
            Files.write(temporary, contents);
            try {
                Files.move(temporary, path, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
            } catch (AtomicMoveNotSupportedException error) {
                Files.move(temporary, path, StandardCopyOption.REPLACE_EXISTING);
            }
        } finally {
            Files.deleteIfExists(temporary);
        }
    }

    static void setOwnerOnlyMode(final Path path) throws IOException {
        try {
            Files.setPosixFilePermissions(path, OWNER_READ_WRITE);
        } catch (UnsupportedOperationException error) {
            // Non-POSIX file systems rely on the parent directory's access control.
        }
    }

    private static boolean hasOwnerOnlyMode(final Path path) throws IOException {
        try {
            return PosixFilePermissions.toString(Files.getPosixFilePermissions(path, LinkOption.NOFOLLOW_LINKS))
                    .equals("rw-------");
        } catch (UnsupportedOperationException error) {
            return true;
        }
    }
}
