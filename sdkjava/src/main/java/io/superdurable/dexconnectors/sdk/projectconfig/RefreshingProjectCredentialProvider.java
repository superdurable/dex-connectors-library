// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.RefreshingCredentialProvider;
import io.superdurable.dexconnectors.sdk.RejectedCredentialRefreshingProvider;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.security.SecureRandom;
import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.Function;

/**
 * Refreshes project credentials once per generation across replicas.
 *
 * <p>For ordinary entries, a conditional head write grants one caller provider dispatch.
 * Observers recover the immutable result or restore an abandoned admission. Shared entries use
 * the broker without consulting the refresh driver.
 *
 * <pre>{@code
 * RefreshingProjectCredentialProvider<GmailCredentials> provider = new RefreshingProjectCredentialProvider<>(
 *         project.getConnections(), new ConnectionKey("google-gmail", "main"),
 *         GmailCredentials::fromJson, GmailCredentials::toJson);
 * GmailCredentials credentials = Credentials.resolveCredential(provider, call, refreshDriver);
 * }</pre>
 *
 * @param <C> the connector-specific credential type
 */
public final class RefreshingProjectCredentialProvider<C> extends ProjectCredentialProvider<C>
        implements RefreshingCredentialProvider<C>, RejectedCredentialRefreshingProvider<C> {
    private static final Duration REFRESH_TIMEOUT = Duration.ofSeconds(30);
    private static final Duration ADMISSION_TIMEOUT = Duration.ofSeconds(40);
    private static final long OBSERVER_POLL_MILLISECONDS = 100;
    private static final SecureRandom RANDOM = new SecureRandom();

    private final Function<C, ObjectNode> encode;

    /**
     * Binds one connection with private-material codecs.
     *
     * @param store the connection store
     * @param key the connection key
     * @param decode validates and converts private material
     * @param encode converts refreshed credentials to complete private material
     */
    public RefreshingProjectCredentialProvider(
            final ConnectionStore store, final ConnectionKey key, final Function<ObjectNode, C> decode,
            final Function<C, ObjectNode> encode) {
        this(store, key, decode, encode, null);
    }

    /**
     * Binds one connection with private-material codecs and a shared-credential decoder.
     *
     * @param store the connection store
     * @param key the connection key
     * @param decode validates and converts private material
     * @param encode converts refreshed credentials to complete private material
     * @param decodeShared validates broker material, or {@code null} to use {@code decode}
     */
    public RefreshingProjectCredentialProvider(
            final ConnectionStore store, final ConnectionKey key, final Function<ObjectNode, C> decode,
            final Function<C, ObjectNode> encode, final Function<ObjectNode, C> decodeShared) {
        super(store, key, decode, decodeShared);
        if (encode == null) {
            throw new IllegalArgumentException("project credential encoder is required");
        }
        this.encode = encode;
    }

    /**
     * Refreshes expired or driver-stale material after winning the durable head CAS.
     *
     * @param call the call identity
     * @param driver the connector-specific refresh driver
     * @return the current or refreshed credential
     * @throws ReauthorizationRequiredException if the connection needs authorization
     * @throws ExchangePendingException if another authorization exchange is still running
     * @throws RefreshFailedException if refresh failed and the prior material is kept
     * @throws IOException if storage fails
     * @throws InterruptedException if the refresh is interrupted
     */
    @Override
    public C resolveWithRefresh(final Call call, final CredentialRefreshDriver<C> driver)
            throws IOException, InterruptedException {
        validateCall(call);
        if (driver == null) {
            throw new IllegalArgumentException("credential refresh driver is required");
        }
        if (shared != null) {
            return decodeSharedMaterial(shared.resolve(call.getId().value()));
        }
        while (true) {
            final ObjectNode record = store.readRecord(key).record;
            final String status = ProjectConfigurationRules.text(record, "status");
            if ("REVOKED".equals(status) || "REAUTHORIZATION_REQUIRED".equals(status)) {
                throw new ReauthorizationRequiredException();
            }
            if ("AUTHORIZING".equals(status) || "REFRESHING".equals(status)) {
                if ("AUTHORIZING".equals(status)) {
                    final Instant deadline = ConnectionStore.parseTime(record.get("mutation").get("deadline"));
                    if (deadline != null && Instant.now().isBefore(deadline)) {
                        throw new ExchangePendingException("credential authorization exchange is pending");
                    }
                }
                try {
                    recoverOrWait(record);
                } catch (ObjectConflictException conflict) {
                    // Another replica moved the head first; read it again.
                }
                continue;
            }
            if (!"READY".equals(status)) {
                throw new IllegalArgumentException("project credential status is invalid");
            }
            final CredentialMaterial material = store.readMaterial(key, record);
            final C credentials = decodeMaterial(material);
            final Instant now = Instant.now();
            final Instant expiresAt = material.getExpiresAtInstant();
            final CredentialRefreshDriver.State<C> state = new CredentialRefreshDriver.State<>(credentials, expiresAt, now);
            if ((expiresAt == null || expiresAt.isAfter(now)) && !driver.isRefreshRequired(state)) {
                return credentials;
            }
            final Admission admission;
            try {
                admission = admitRefresh(record, now);
            } catch (ObjectConflictException conflict) {
                continue;
            }
            return refreshAdmitted(admission, material, state, driver);
        }
    }

    /**
     * Refreshes after a provider rejection only when the stored expiry has elapsed.
     *
     * <p>A shared connection asks the broker for a newer revision instead.
     *
     * @param call the call identity
     * @param driver the connector-specific refresh driver
     * @return the refreshed credential
     * @throws RejectedCredentialNotRefreshedException if the stored credential has not expired
     * @throws IOException if storage or the exchange fails
     * @throws InterruptedException if the refresh is interrupted
     */
    @Override
    public C resolveAfterRejection(final Call call, final CredentialRefreshDriver<C> driver)
            throws IOException, InterruptedException {
        validateCall(call);
        if (driver == null) {
            throw new IllegalArgumentException("credential refresh driver is required");
        }
        if (shared != null) {
            return decodeSharedMaterial(shared.resolveAfterRejection(call.getId().value()));
        }
        final ProjectConnection connection = store.readConnection(key);
        if (connection.getExpiresAt() == null || connection.getExpiresAt().isAfter(Instant.now())) {
            throw new RejectedCredentialNotRefreshedException("authentication rejection does not prove credential expiry");
        }
        return resolveWithRefresh(call, driver);
    }

    Admission admitRefresh(final ObjectNode record, final Instant now) throws IOException {
        final String attemptId = randomHex();
        final String path = key.path(store.getScope());
        final Instant deadline = now.plus(ADMISSION_TIMEOUT);
        final long fence = record.get("fence").longValue() + 1;
        final ObjectNode mutation = GoCompatibleJson.newObject();
        mutation.put("kind", "REFRESHING");
        mutation.put("invocationId", randomHex());
        mutation.put("id", attemptId);
        mutation.put("fence", fence);
        mutation.put("baseRevision", record.get("revision").longValue());
        mutation.put("deadline", Rfc3339NanoTimestamp.fromInstant(deadline).toWire());
        mutation.put("resultKey", path + "/exchange-results/" + attemptId);
        final ObjectNode updated = ConnectionDocuments.connectionRecord(store.getScope(), key,
                record.get("revision").longValue() + 1, fence, "REFRESHING", expiry(record.get("expiresAt")),
                ProjectConfigurationRules.text(record, "authMethod"),
                GoCompatibleJson.requireObject(record.get("credential"), "credential reference"), mutation);
        writeHead(record, updated);
        return new Admission("REFRESHING", attemptId, fence, deadline, path + "/exchange-results/" + attemptId);
    }

    private C refreshAdmitted(
            final Admission admission, final CredentialMaterial prior, final CredentialRefreshDriver.State<C> state,
            final CredentialRefreshDriver<C> driver) throws IOException, InterruptedException {
        final CredentialRefreshDriver.Result<C> result;
        try {
            result = refreshWithTimeout(driver, state);
        } catch (ReauthorizationRequiredException error) {
            finishExchange(admission, "REAUTHORIZATION_REQUIRED");
            throw error;
        } catch (InterruptedException error) {
            finishExchange(admission, "READY");
            throw error;
        } catch (Exception error) {
            finishExchange(admission, "READY");
            throw new RefreshFailedException("project credential refresh failed; current authorization is kept");
        }
        if (!result.getExpiresAt().isAfter(Instant.now())) {
            finishExchange(admission, "READY");
            throw new RefreshFailedException("refreshed credential has no future expiry");
        }
        final ObjectNode encoded;
        try {
            encoded = encode.apply(result.getCredentials());
            if (encoded == null || encoded.size() == 0) {
                throw new IllegalArgumentException("invalid private credential material");
            }
        } catch (RuntimeException error) {
            finishExchange(admission, "READY");
            throw new RefreshFailedException("refreshed credential encoding failed");
        }
        commitRefresh(admission, new CredentialMaterial(encoded, Rfc3339NanoTimestamp.fromInstant(result.getExpiresAt()),
                prior.getAuthMethod()));
        return result.getCredentials();
    }

    private CredentialRefreshDriver.Result<C> refreshWithTimeout(
            final CredentialRefreshDriver<C> driver, final CredentialRefreshDriver.State<C> state) throws Exception {
        final ExecutorService executor = Executors.newSingleThreadExecutor(runnable -> {
            final Thread thread = new Thread(runnable, "dex-connector-credential-refresh");
            thread.setDaemon(true);
            return thread;
        });
        try {
            final Future<CredentialRefreshDriver.Result<C>> future = executor.submit(() -> driver.refresh(state));
            try {
                return future.get(REFRESH_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
            } catch (TimeoutException error) {
                future.cancel(true);
                throw error;
            } catch (InterruptedException error) {
                future.cancel(true);
                throw error;
            } catch (ExecutionException error) {
                final Throwable cause = error.getCause();
                if (cause instanceof Exception) {
                    throw (Exception) cause;
                }
                throw new IOException("credential refresh failed");
            }
        } finally {
            executor.shutdownNow();
        }
    }

    void recoverOrWait(final ObjectNode record) throws IOException, InterruptedException {
        final ObjectNode mutation = GoCompatibleJson.requireObject(record.get("mutation"), "credential mutation");
        final Instant deadline = ConnectionStore.parseTime(mutation.get("deadline"));
        final Admission admission = new Admission(ProjectConfigurationRules.text(record, "status"),
                ProjectConfigurationRules.text(mutation, "id"), record.get("fence").longValue(),
                deadline == null ? Instant.MIN : deadline, ProjectConfigurationRules.text(mutation, "resultKey"));
        final ProjectObject result;
        try {
            result = store.getObjects().readObject(admission.resultKey, "");
        } catch (ObjectNotFoundException error) {
            if (Instant.now().isBefore(admission.deadline)) {
                Thread.sleep(OBSERVER_POLL_MILLISECONDS);
                return;
            }
            finishExchange(admission, "AUTHORIZING".equals(admission.kind) ? "REAUTHORIZATION_REQUIRED" : "READY");
            return;
        }
        publishResult(admission, result);
    }

    private void commitRefresh(final Admission admission, final CredentialMaterial material) throws IOException {
        final byte[] contents = GoCompatibleJson.encode(ConnectionDocuments.credentialEnvelope(store.getScope(), key,
                admission.fence, material.getCredentials(), material.getExpiresAt(), material.getAuthMethod(),
                admission.attemptId));
        ProjectObject result;
        try {
            result = store.getObjects().createObject(admission.resultKey, contents);
        } catch (ObjectConflictException | OutcomeUnknownException error) {
            result = store.getObjects().readObject(admission.resultKey, "");
            if (!result.hasContents(contents)) {
                throw new IllegalArgumentException("immutable credential result differs");
            }
        }
        publishResult(admission, result);
    }

    private void publishResult(final Admission admission, final ProjectObject result) throws IOException {
        final ObjectNode record = store.readRecord(key).record;
        if (isReadyWithCredential(record, result.getKey())) {
            return;
        }
        requireAdmission(record, admission);
        final ObjectNode envelope = GoCompatibleJson.parseObject(result.getContents(), "credential result");
        if (!ConnectionDocuments.CONNECTION_SCHEMA_VERSION.equals(ProjectConfigurationRules.text(envelope, "schemaVersion"))
                || !store.getScope().toJson().equals(envelope.get("scope"))
                || !key.toJson().equals(envelope.get("connection"))
                || envelope.get("fence") == null || !envelope.get("fence").isIntegralNumber()
                || envelope.get("fence").longValue() != admission.fence
                || !admission.attemptId.equals(ProjectConfigurationRules.text(envelope, "attemptId"))) {
            throw new IllegalArgumentException("credential exchange result identity is invalid");
        }
        final ObjectNode reference = GoCompatibleJson.newObject();
        reference.put("key", result.getKey());
        reference.put("version", result.getVersion());
        reference.put("digest", GoCompatibleJson.sha256Hex(result.getContents()));
        final ObjectNode updated = ConnectionDocuments.connectionRecord(store.getScope(), key,
                record.get("revision").longValue() + 1, record.get("fence").longValue(), "READY",
                expiry(envelope.get("expiresAt")), ProjectConfigurationRules.text(envelope, "authMethod"), reference,
                null);
        try {
            writeHead(record, updated);
        } catch (ObjectConflictException conflict) {
            if (!isReadyWithCredential(store.readRecord(key).record, result.getKey())) {
                throw conflict;
            }
        }
    }

    private void finishExchange(final Admission admission, final String status) throws IOException {
        final ObjectNode record = store.readRecord(key).record;
        requireAdmission(record, admission);
        final JsonNode credential = record.get("credential");
        final ObjectNode updated = ConnectionDocuments.connectionRecord(store.getScope(), key,
                record.get("revision").longValue() + 1, record.get("fence").longValue() + 1, status,
                expiry(record.get("expiresAt")), ProjectConfigurationRules.text(record, "authMethod"),
                credential == null || credential.isNull() ? null
                        : GoCompatibleJson.requireObject(credential, "credential reference"),
                null);
        writeHead(record, updated);
    }

    private void writeHead(final ObjectNode prior, final ObjectNode updated) throws IOException {
        final String headPath = key.path(store.getScope()) + "/head";
        final ConnectionStore.HeadRecord head = store.readRecord(key);
        if (!head.record.equals(prior)) {
            throw new ObjectConflictException("connection revision conflict");
        }
        final byte[] contents = GoCompatibleJson.encode(updated);
        try {
            store.getObjects().compareAndSwapObject(headPath, head.object.getEtag(), contents);
        } catch (OutcomeUnknownException unknown) {
            if (!store.getObjects().readObject(headPath, "").hasContents(contents)) {
                throw unknown;
            }
        }
    }

    private static boolean isReadyWithCredential(final ObjectNode record, final String resultKey) {
        final JsonNode credential = record.get("credential");
        return "READY".equals(ProjectConfigurationRules.text(record, "status")) && credential != null
                && credential.isObject() && resultKey.equals(ProjectConfigurationRules.text(credential, "key"));
    }

    private static void requireAdmission(final ObjectNode record, final Admission admission)
            throws ObjectConflictException {
        final JsonNode mutation = record.get("mutation");
        if (!admission.kind.equals(ProjectConfigurationRules.text(record, "status"))
                || record.get("fence").longValue() != admission.fence || mutation == null || !mutation.isObject()
                || !admission.attemptId.equals(ProjectConfigurationRules.text(mutation, "id"))
                || !admission.kind.equals(ProjectConfigurationRules.text(mutation, "kind"))) {
            throw new ObjectConflictException("credential exchange admission was superseded");
        }
    }

    private static Rfc3339NanoTimestamp expiry(final JsonNode value) {
        if (value == null || value.isNull()) {
            return null;
        }
        if (!value.isTextual()) {
            throw new IllegalArgumentException("invalid credential expiry");
        }
        return Rfc3339NanoTimestamp.fromWire(value.textValue());
    }

    private static String randomHex() {
        final byte[] bytes = new byte[16];
        RANDOM.nextBytes(bytes);
        final StringBuilder hex = new StringBuilder();
        for (byte part : bytes) {
            hex.append(String.format("%02x", part & 0xff));
        }
        return hex.toString();
    }

    static final class Admission {
        final String kind;
        final String attemptId;
        final long fence;
        final Instant deadline;
        final String resultKey;

        Admission(final String kind, final String attemptId, final long fence, final Instant deadline,
                final String resultKey) {
            this.kind = kind;
            this.attemptId = attemptId;
            this.fence = fence;
            this.deadline = deadline;
            this.resultKey = resultKey;
        }
    }
}
