// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Project object store backed by process memory and Dex Web's owner-only {@code connections.json}.
 *
 * <p>Connection head reads adopt credentials Dex Web saved, and fail with
 * {@link ConnectionRemovedFromFileException} once the connection is no longer listed in the file,
 * judged from the same read. A successful head write publishes refreshed material back to the
 * file only when Dex Web has not saved another credential since. A credential refresh holds the
 * cross-process {@code connections.json.lock}, rereads the file, and proves that the write-back
 * can succeed before the provider exchange, as Go's SDK does; see {@link DexWebRefreshLock} for
 * platform coverage. An exchange with an unknown outcome leaves a marker in
 * {@code refresh-quarantine/} beside the file, bound to that credential, so no store or restarted
 * process exchanges it again until Dex Web saves a new authorization. Trigger inboxes live in
 * private files beside the connections file.
 * Thread-safe.
 */
public final class DexWebObjectStore implements ObjectStore {
    private static final Logger LOG = Logger.getLogger(DexWebObjectStore.class.getName());

    private final Path connectionsPath;
    private final Scope scope;
    private final LocalFileObjectStore inboxes;
    private final InMemoryObjectStore memory = new InMemoryObjectStore();
    private final ConnectionStore connections;
    private final DexWebRefreshLock refreshLock;
    private final Map<ConnectionKey, String> importedDigests = new HashMap<>();

    /**
     * Binds the Dex Web connections file; call {@link #initialize()} before use.
     *
     * @param connectionsPath the owner-only {@code connections.json} path
     * @param scope the local project scope
     */
    public DexWebObjectStore(final Path connectionsPath, final Scope scope) {
        if (connectionsPath == null || scope == null) {
            throw new IllegalArgumentException("Dex Web connections path and scope are required");
        }
        this.connectionsPath = connectionsPath.toAbsolutePath();
        this.scope = scope;
        this.inboxes = new LocalFileObjectStore(this.connectionsPath.getParent().resolve("trigger-inboxes"));
        this.connections = new ConnectionStore(memory, scope);
        this.refreshLock = new DexWebRefreshLock(this.connectionsPath, DexWebRefreshLock.MAXIMUM_WAIT);
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
                requireListed(key, synchronizeFromFile());
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
            requireListed(key, synchronizeFromFile());
            final ProjectObject result = memory.compareAndSwapObject(key, expectedEtag, contents);
            writeBack(result);
            return result;
        }
    }

    /** Holds the cross-process refresh lock of {@code connections.json} until the result is closed. */
    DexWebRefreshLock.Held lockCredentialRefresh() throws IOException, InterruptedException {
        return refreshLock.acquire();
    }

    /**
     * Proves, under the refresh lock, that a refreshed credential of the key can be written back.
     *
     * <p>One read supplies the checked record and the bytes rewritten unchanged through the
     * write-back path, so a read-only file or directory fails before a provider rotates a token.
     *
     * @throws FileChangedSinceReadException when the file holds another credential than the
     *     one last synchronized; read the record again
     * @throws ConnectionRemovedFromFileException when the connection is no longer listed
     */
    synchronized void verifyCredentialWriteBack(final ConnectionKey key) throws IOException {
        final byte[] contents = DexWebFiles.readPrivateFileBytes(connectionsPath);
        final ObjectNode source = GoCompatibleJson.parseObject(contents, connectionsPath.getFileName().toString());
        final CredentialMaterial listed = DexWebFiles.decodeLocalCredentials(source).get(key);
        if (listed == null) {
            throw new ConnectionRemovedFromFileException();
        }
        if (!DexWebFiles.credentialDigest(listed).equals(importedDigests.get(key))) {
            throw new FileChangedSinceReadException();
        }
        // A writer that skips the lock could replace the file after this read; never overwrite its write.
        DexWebFiles.writePrivateFileChecked(connectionsPath, contents, () -> {
            if (!Arrays.equals(Files.readAllBytes(connectionsPath), contents)) {
                throw new FileChangedSinceReadException();
            }
        });
    }

    /**
     * Records, under the refresh lock, that the current file credential of a key may already have
     * been rotated by an exchange whose outcome is unknown.
     *
     * <p>The marker lives beside {@code connections.json}, so every store on the file and every
     * restarted process sees it, and it is bound to the credential digest: once Dex Web saves a new
     * authorization the marker no longer applies.
     */
    synchronized void quarantineCurrentCredential(final ConnectionKey key) throws IOException {
        final String digest = importedDigests.get(key);
        if (digest == null) {
            return;
        }
        // The marker holds only a credential digest; it is written owner-only like the credentials file.
        Files.createDirectories(quarantineDirectory());
        DexWebFiles.writePrivateFile(quarantineMarker(key), digest.getBytes(StandardCharsets.US_ASCII));
    }

    /**
     * Reports, under the refresh lock, whether the current file credential of a key must not be
     * exchanged again because an earlier exchange of it has an unknown outcome.
     *
     * <p>A marker for an older credential is removed, since Dex Web saved a new authorization.
     */
    synchronized boolean isCurrentCredentialQuarantined(final ConnectionKey key) throws IOException {
        final Path marker = quarantineMarker(key);
        final byte[] contents;
        try {
            contents = DexWebFiles.readPrivateFileBytes(marker);
        } catch (NoSuchFileException missing) {
            return false;
        } catch (IllegalArgumentException invalid) {
            // An unreadable marker cannot prove the credential is safe to exchange.
            return true;
        }
        if (new String(contents, StandardCharsets.US_ASCII).equals(importedDigests.get(key))) {
            return true;
        }
        Files.deleteIfExists(marker);
        return false;
    }

    private Path quarantineDirectory() {
        return connectionsPath.getParent().resolve("refresh-quarantine");
    }

    private Path quarantineMarker(final ConnectionKey key) {
        return quarantineDirectory().resolve(GoCompatibleJson.sha256Hex(
                (key.getConnectorId() + "\u0000" + key.getConnectionName()).getBytes(StandardCharsets.UTF_8)));
    }

    private Set<ConnectionKey> synchronizeFromFile() throws IOException {
        final ObjectNode source = DexWebFiles.readPrivateFile(connectionsPath);
        final Map<ConnectionKey, CredentialMaterial> listed = DexWebFiles.decodeLocalCredentials(source);
        for (Map.Entry<ConnectionKey, CredentialMaterial> entry : listed.entrySet()) {
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
        // A removed connection forgets its digest, so a recreated one with equal credentials imports again.
        importedDigests.keySet().retainAll(listed.keySet());
        return listed.keySet();
    }

    private void requireListed(final String headKey, final Set<ConnectionKey> listed) throws IOException {
        for (ConnectionKey key : listed) {
            if (headKey.equals(key.path(scope) + "/head")) {
                return;
            }
        }
        throw new ConnectionRemovedFromFileException();
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
