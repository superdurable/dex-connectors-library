// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;

/**
 * Builds the Go-ordered connection head and credential envelope documents.
 *
 * <p>Encode the results with {@link GoCompatibleJson#encode}; their bytes identify immutable
 * credential objects and CAS heads shared with Go, Python, and TypeScript replicas.
 */
public final class ConnectionDocuments {
    /** Connection head and credential envelope schema. */
    public static final String CONNECTION_SCHEMA_VERSION = "connectors.dex.dev/project-connection/v1alpha2";

    private ConnectionDocuments() {
    }

    /**
     * Builds a private credential envelope.
     *
     * @param scope the project scope
     * @param key the connection key
     * @param fence the credential fence
     * @param credentials the private credential object
     * @param expiresAt the expiry, or {@code null}
     * @param authMethod the authorization method ID
     * @param attemptId the exchange attempt ID, or empty for a direct replacement
     * @return the envelope in Go field order
     */
    public static ObjectNode credentialEnvelope(
            final Scope scope, final ConnectionKey key, final long fence, final ObjectNode credentials,
            final Rfc3339NanoTimestamp expiresAt, final String authMethod, final String attemptId) {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("schemaVersion", CONNECTION_SCHEMA_VERSION);
        result.set("scope", scope.toJson());
        result.set("connection", key.toJson());
        result.put("fence", fence);
        if (attemptId != null && !attemptId.isEmpty()) {
            result.put("attemptId", attemptId);
        }
        result.set("credentials", credentials.deepCopy());
        if (expiresAt != null) {
            result.put("expiresAt", expiresAt.toWire());
        }
        result.put("authMethod", authMethod);
        return result;
    }

    /**
     * Builds a connection head record.
     *
     * @param scope the project scope
     * @param key the connection key
     * @param revision the head revision
     * @param fence the credential fence
     * @param status the connection status
     * @param expiresAt the expiry, or {@code null}
     * @param authMethod the authorization method ID
     * @param credentialReference the {@code key}, {@code version}, and {@code digest} of the
     *     credential object, or {@code null}
     * @param mutation the admitted credential exchange, or {@code null}
     * @return the record in Go field order
     */
    public static ObjectNode connectionRecord(
            final Scope scope, final ConnectionKey key, final long revision, final long fence, final String status,
            final Rfc3339NanoTimestamp expiresAt, final String authMethod, final JsonNode credentialReference,
            final JsonNode mutation) {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("schemaVersion", CONNECTION_SCHEMA_VERSION);
        result.set("connection", key.toJson());
        result.set("scope", scope.toJson());
        result.put("revision", revision);
        result.put("fence", fence);
        result.put("status", status);
        if (expiresAt != null) {
            result.put("expiresAt", expiresAt.toWire());
        }
        result.put("authMethod", authMethod);
        if (credentialReference != null && !credentialReference.isNull()) {
            final ObjectNode credential = result.putObject("credential");
            credential.set("key", credentialReference.get("key"));
            credential.set("version", credentialReference.get("version"));
            credential.set("digest", credentialReference.get("digest"));
        }
        if (mutation != null && !mutation.isNull()) {
            final ObjectNode written = result.putObject("mutation");
            written.set("kind", mutation.get("kind"));
            written.set("invocationId", mutation.get("invocationId"));
            written.set("id", mutation.get("id"));
            written.set("fence", mutation.get("fence"));
            written.set("baseRevision", mutation.get("baseRevision"));
            written.put("deadline", Rfc3339NanoTimestamp.fromWire(mutation.get("deadline").asText()).toWire());
            written.set("resultKey", mutation.get("resultKey"));
        }
        return result;
    }
}
