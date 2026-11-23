// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Project object store backed by process memory and Dex Web's owner-only {@code connections.json}.
 *
 * <p>Connection head reads adopt credentials Dex Web saved. A successful head write publishes
 * refreshed material back to the file only when Dex Web has not saved another credential since,
 * matching Go's process-local synchronization. Trigger inboxes live in private files beside the
 * connections file. Thread-safe.
 */
public final class DexWebObjectStore implements ObjectStore {
    private static final Logger LOG = Logger.getLogger(DexWebObjectStore.class.getName());

    private final Path connectionsPath;
    private final LocalFileObjectStore inboxes;
    private final InMemoryObjectStore memory = new InMemoryObjectStore();
    private final ConnectionStore connections;
    private final Map<ConnectionKey, String> importedDigests = new HashMap<>();

    /**
     * Binds the Dex Web connections file; call {@link #initialize()} before use.
     *
     * @param connectionsPath the owner-only {@code connections.json} path
     * @param scope the local project scope
     */
    public DexWebObjectStore(final Path connectionsPath, final Scope scope) {
        if (connectionsPath == null) {
            throw new IllegalArgumentException("Dex Web connections path is required");
        }
        this.connectionsPath = connectionsPath.toAbsolutePath();
        this.inboxes = new LocalFileObjectStore(this.connectionsPath.getParent().resolve("trigger-inboxes"));
        this.connections = new ConnectionStore(memory, scope);
    }

    /**
     * Loads local Trigger inboxes, then imports current private credentials.
     *
     * @throws IOException if a file cannot be read
     */
    public void initialize() throws IOException {
        inboxes.initialize();
        synchronized (this) {
            synchronizeFromFile();
        }
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject readObject(final String key, final String version) throws IOException {
        if (isTriggerInboxKey(key)) {
            return inboxes.readObject(key, version);
        }
        if (isConnectionHead(key)) {
            synchronized (this) {
                synchronizeFromFile();
                return memory.readObject(key, version);
            }
        }
        return memory.readObject(key, version);
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
        if (isTriggerInboxKey(key)) {
            return inboxes.createObject(key, contents);
        }
        return memory.createObject(key, contents);
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject compareAndSwapObject(final String key, final String expectedEtag, final byte[] contents)
            throws IOException {
        if (isTriggerInboxKey(key)) {
            return inboxes.compareAndSwapObject(key, expectedEtag, contents);
        }
        if (!isConnectionHead(key)) {
            return memory.compareAndSwapObject(key, expectedEtag, contents);
        }
        synchronized (this) {
            synchronizeFromFile();
            final ProjectObject result = memory.compareAndSwapObject(key, expectedEtag, contents);
            writeBack(result);
            return result;
        }
    }

    private void synchronizeFromFile() throws IOException {
        final ObjectNode source = DexWebFiles.readPrivateFile(connectionsPath);
        for (Map.Entry<ConnectionKey, CredentialMaterial> entry : DexWebFiles.decodeLocalCredentials(source).entrySet()) {
            final String digest = DexWebFiles.credentialDigest(entry.getValue());
            if (digest.equals(importedDigests.get(entry.getKey()))) {
                continue;
            }
            long revision;
            try {
                revision = connections.readConnection(entry.getKey()).getRevision();
            } catch (ObjectNotFoundException error) {
                revision = 0;
            }
            connections.replaceCredential(entry.getKey(), revision, entry.getValue());
            importedDigests.put(entry.getKey(), digest);
        }
    }

    private void writeBack(final ProjectObject head) {
        try {
            final ObjectNode record = GoCompatibleJson.parseObject(head.getContents(), "connection head");
            final JsonNode credential = record.get("credential");
            if (!"READY".equals(ProjectConfigurationRules.text(record, "status"))
                    || credential == null || !credential.isObject()) {
                return;
            }
            final JsonNode identity = GoCompatibleJson.requireObject(record.get("connection"), "connection identity");
            final ConnectionKey key = new ConnectionKey(ProjectConfigurationRules.text(identity, "connectorId"),
                    ProjectConfigurationRules.text(identity, "connectionName"));
            final CredentialMaterial material = connections.readCredentialMaterial(key).getMaterial();
            final String digest = DexWebFiles.credentialDigest(material);
            if (digest.equals(importedDigests.get(key))) {
                return;
            }
            replaceInFile(key, material);
            importedDigests.put(key, digest);
        } catch (IOException | RuntimeException error) {
            LOG.log(Level.WARNING, "local connector credential write-back skipped");
        }
    }

    private void replaceInFile(final ConnectionKey key, final CredentialMaterial material) throws IOException {
        final ObjectNode source = DexWebFiles.readPrivateFile(connectionsPath);
        final JsonNode records = source.get("connections");
        if (records == null || !records.isArray()) {
            throw new IllegalArgumentException("connections.json requires connections");
        }
        for (JsonNode raw : records) {
            if (!raw.isObject() || !key.getConnectorId().equals(ProjectConfigurationRules.text(raw, "connectorId"))
                    || !key.getConnectionName().equals(ProjectConfigurationRules.text(raw, "connectionName"))) {
                continue;
            }
            final ObjectNode record = (ObjectNode) raw;
            final ObjectNode prior = GoCompatibleJson.requireObject(record.get("credentials"), "credentials");
            final Rfc3339NanoTimestamp priorExpiry = DexWebFiles.localExpiry(record.get("credentialExpiresAt"));
            final String priorDigest = DexWebFiles.credentialDigest(
                    new CredentialMaterial(prior, priorExpiry, material.getAuthMethod()));
            if (!priorDigest.equals(importedDigests.get(key))) {
                throw new IllegalArgumentException("the file holds a newer credential");
            }
            record.set("credentials", material.getCredentials());
            if (material.getExpiresAt() == null) {
                record.remove("credentialExpiresAt");
            } else {
                record.put("credentialExpiresAt",
                        Rfc3339NanoTimestamp.fromInstant(material.getExpiresAtInstant()).toWire());
            }
            DexWebFiles.writePrivateFile(connectionsPath, DexWebFiles.prettyJson(source));
            return;
        }
        throw new IllegalArgumentException("the connection is no longer in the file");
    }

    private static boolean isTriggerInboxKey(final String key) {
        return key.contains("/trigger-inboxes/");
    }

    private static boolean isConnectionHead(final String key) {
        return key.contains("/connections/") && key.endsWith("/head");
    }
}
