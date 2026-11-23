// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Pattern;

/**
 * Versioned objects kept in memory and as owner-only files in the Go and TypeScript local layout.
 *
 * <p>Each object is {@code <sha256 of key>-<version>.json} holding its key, version, ETag, and
 * base64 contents. Only the latest version of each key is kept. Thread-safe.
 */
final class LocalFileObjectStore implements ObjectStore {
    private static final Pattern LOCAL_VERSION = Pattern.compile("^[1-9][0-9]*$");

    private final Path directory;
    private final Map<String, ProjectObject> objects = new HashMap<>();

    LocalFileObjectStore(final Path directory) {
        this.directory = directory;
    }

    synchronized void initialize() throws IOException {
        Files.createDirectories(directory);
        final Map<String, List<ProjectObject>> loaded = new HashMap<>();
        try (DirectoryStream<Path> files = Files.newDirectoryStream(directory, "*.json")) {
            for (Path path : files) {
                if (path.getFileName().toString().startsWith(".")) {
                    continue;
                }
                final ProjectObject item = decodeLocalObject(Files.readAllBytes(path));
                loaded.computeIfAbsent(item.getKey(), ignored -> new ArrayList<>()).add(item);
            }
        }
        for (Map.Entry<String, List<ProjectObject>> entry : loaded.entrySet()) {
            final List<ProjectObject> versions = entry.getValue();
            versions.sort((left, right) -> Long.compare(Long.parseLong(left.getVersion()),
                    Long.parseLong(right.getVersion())));
            // A crash between writing a new version and deleting the old one leaves both.
            for (ProjectObject stale : versions.subList(0, versions.size() - 1)) {
                removeFile(stale);
            }
            objects.put(entry.getKey(), versions.get(versions.size() - 1));
        }
    }

    @Override
    public synchronized ProjectObject readObject(final String key, final String version) throws ObjectNotFoundException {
        final ProjectObject item = objects.get(key);
        if (item == null || (version != null && !version.isEmpty() && !item.getVersion().equals(version))) {
            throw new ObjectNotFoundException("project object not found");
        }
        return item;
    }

    @Override
    public synchronized ProjectObject createObject(final String key, final byte[] contents) throws IOException {
        if (objects.containsKey(key)) {
            throw new ObjectConflictException("project object already exists");
        }
        return write(key, contents, null);
    }

    @Override
    public synchronized ProjectObject compareAndSwapObject(
            final String key, final String expectedEtag, final byte[] contents) throws IOException {
        final ProjectObject current = objects.get(key);
        if (current == null) {
            throw new ObjectNotFoundException("project object not found");
        }
        if (expectedEtag == null || expectedEtag.isEmpty() || !current.getEtag().equals(expectedEtag)) {
            throw new ObjectConflictException("project object compare-and-swap conflict");
        }
        return write(key, contents, current);
    }

    private ProjectObject write(final String key, final byte[] contents, final ProjectObject previous)
            throws IOException {
        // The new version is durable before the superseded file is deleted, so a crash never loses the key.
        InMemoryObjectStore.validateObjectKey(key);
        if (contents == null || contents.length == 0
                || contents.length > InMemoryObjectStore.localObjectByteLimit(key)) {
            throw new IllegalArgumentException("project object body is empty or oversized");
        }
        final String version = Long.toString(previous == null ? 1 : Long.parseLong(previous.getVersion()) + 1);
        final ProjectObject item = new ProjectObject(key, version,
                GoCompatibleJson.digestBytes(contents) + "-" + version, contents);
        final ObjectNode document = GoCompatibleJson.newObject();
        document.put("key", key);
        document.put("version", version);
        document.put("etag", item.getEtag());
        document.put("contents", Base64.getEncoder().encodeToString(contents));
        DexWebFiles.writePrivateFile(file(item), GoCompatibleJson.encode(document));
        objects.put(key, item);
        if (previous != null) {
            removeFile(previous);
        }
        return item;
    }

    private Path file(final ProjectObject item) {
        return directory.resolve(GoCompatibleJson.sha256Hex(item.getKey().getBytes(StandardCharsets.UTF_8))
                + "-" + item.getVersion() + ".json");
    }

    private void removeFile(final ProjectObject item) {
        try {
            Files.deleteIfExists(file(item));
        } catch (IOException error) {
            // A failure leaves a stale file that the next load discards.
        }
    }

    private static ProjectObject decodeLocalObject(final byte[] contents) {
        try {
            final ObjectNode record = GoCompatibleJson.parseObject(contents, "local object");
            final JsonNode key = record.get("key");
            final JsonNode version = record.get("version");
            final JsonNode etag = record.get("etag");
            final JsonNode encoded = record.get("contents");
            if (key == null || !key.isTextual() || key.textValue().isEmpty() || etag == null || !etag.isTextual()
                    || etag.textValue().isEmpty() || version == null || !version.isTextual()
                    || !LOCAL_VERSION.matcher(version.textValue()).matches()
                    || encoded == null || !encoded.isTextual()) {
                throw new IllegalArgumentException("invalid fields");
            }
            return new ProjectObject(key.textValue(), version.textValue(), etag.textValue(),
                    Base64.getDecoder().decode(encoded.textValue()));
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("local project storage file is invalid");
        }
    }
}
