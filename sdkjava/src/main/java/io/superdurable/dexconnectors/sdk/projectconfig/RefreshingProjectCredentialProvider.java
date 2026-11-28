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
import io.superdurable.dexconnectors.sdk.providerhttp.AddressRefusalException;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderTransportException;
import java.io.IOException;
import java.security.SecureRandom;
import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Function;

/**
 * Refreshes project credentials once per generation across replicas.
 *
 * <p>For ordinary entries, a conditional head write grants one caller provider dispatch.
 * Observers recover the immutable result or restore an abandoned admission. Shared entries use
 * the broker without consulting the refresh driver.
 *
 * <p>A driver exchange runs for at most 30 seconds, then is interrupted and given 10 more
 * seconds. A driver that finishes in that grace window is a confirmed result and is stored. One
 * still running afterwards may already have rotated the refresh token, so the connection becomes
 * {@code REAUTHORIZATION_REQUIRED} instead of resubmitting that token. The same holds for any
 * driver failure that does not prove the request never left the process (an address refusal or a
 * {@code ProviderTransportException} that is provably not sent restores {@code READY}), and for an
 * expired refresh admission without a result. The state lives in the shared object store head,
 * so every replica and restarted process honors it until a new authorization replaces it.
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
    // A driver that ignores interruption keeps the refresh lock this long after its timeout; one still
    // running afterwards leaves the connection REAUTHORIZATION_REQUIRED so its token is never resubmitted.
    private static final Duration DRIVER_EXIT_GRACE = Duration.ofSeconds(10);
    private static final SecureRandom RANDOM = new SecureRandom();

    private final Function<C, ObjectNode> encode;
    private final Duration refreshTimeout;
    private final Duration driverExitGrace;

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
        this(store, key, decode, encode, decodeShared, REFRESH_TIMEOUT, DRIVER_EXIT_GRACE);
    }

    RefreshingProjectCredentialProvider(
            final ConnectionStore store, final ConnectionKey key, final Function<ObjectNode, C> decode,
            final Function<C, ObjectNode> encode, final Function<ObjectNode, C> decodeShared,
            final Duration refreshTimeout, final Duration driverExitGrace) {
        super(store, key, decode, decodeShared);
        if (encode == null) {
            throw new IllegalArgumentException("project credential encoder is required");
        }
        this.encode = encode;
        this.refreshTimeout = refreshTimeout;
        this.driverExitGrace = driverExitGrace;
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
        boolean wasInterrupted = false;
        try {
            final DriverRun<C> run = new DriverRun<>(driver, state);
            try {
                run.awaitCompletion(refreshTimeout);
            } catch (InterruptedException interrupted) {
                wasInterrupted = true;
            }
            if (!run.isDone()) {
                run.cancel();
                // Driver completion decides what is stored, so later interrupts do not shorten the grace.
                wasInterrupted |= run.awaitCompletionUninterruptibly(driverExitGrace);
            }
            if (!run.isDone()) {
                // The provider may already have rotated the refresh token, so it is never resubmitted.
                markOutcomeUnknown(admission);
                throw new ReauthorizationRequiredException();
            }
            final CredentialRefreshDriver.Result<C> result;
            try {
                result = run.result();
            } catch (ReauthorizationRequiredException error) {
                markOutcomeUnknown(admission);
                throw error;
            } catch (Exception error) {
                if (!isProvablyNoExchange(error) && !driver.canRepeatFailedExchange(state)) {
                    // A failure after the request may have left the process cannot prove the token survived.
                    markOutcomeUnknown(admission);
                    throw new ReauthorizationRequiredException();
                }
                finishExchange(admission, "READY");
                if (error instanceof AddressRefusalException) {
                    // An address refusal is a configuration fact the caller must see unchanged, as in Python.
                    throw (AddressRefusalException) error;
                }
                throw new RefreshFailedException("project credential refresh failed; current authorization is kept");
            }
            final ObjectNode encoded;
            try {
                if (!result.getExpiresAt().isAfter(Instant.now())) {
                    throw new IllegalArgumentException("refreshed credential has no future expiry");
                }
                encoded = encode.apply(result.getCredentials());
                if (encoded == null || encoded.size() == 0) {
                    throw new IllegalArgumentException("invalid private credential material");
                }
            } catch (RuntimeException error) {
                // The provider answered, so it may have rotated the token this result cannot store.
                markOutcomeUnknown(admission);
                throw new ReauthorizationRequiredException();
            }
            // A driver that finished during the grace window is a confirmed rotation and is stored now.
            commitRefresh(admission, new CredentialMaterial(encoded,
                    Rfc3339NanoTimestamp.fromInstant(result.getExpiresAt()), prior.getAuthMethod()));
            return result.getCredentials();
        } finally {
            if (wasInterrupted) {
                Thread.currentThread().interrupt();
            }
        }
    }

    /**
     * Keeps the connection from exchanging the current refresh token again: the shared head becomes
     * {@code REAUTHORIZATION_REQUIRED}, which every replica reads.
     */
    private void markOutcomeUnknown(final Admission admission) throws IOException {
        finishExchange(admission, "REAUTHORIZATION_REQUIRED");
    }

    /** Reports whether a driver failure proves no request reached the token endpoint. */
    private static boolean isProvablyNoExchange(final Throwable error) {
        if (error instanceof AddressRefusalException) {
            return true;
        }
        for (Throwable current = error; current != null; current = current.getCause()) {
            if (current instanceof ProviderTransportException) {
                return ((ProviderTransportException) current).isProvablyNotSent();
            }
        }
        return false;
    }

    /**
     * One driver exchange on its own daemon thread, with a completion signal independent of the
     * thread's exit.
     *
     * @param <C> the credential type
     */
    private static final class DriverRun<C> {
        private final CountDownLatch completed = new CountDownLatch(1);
        private final Thread thread;
        private volatile CredentialRefreshDriver.Result<C> result;
        private volatile Throwable failure;

        private DriverRun(final CredentialRefreshDriver<C> driver, final CredentialRefreshDriver.State<C> state) {
            thread = new Thread(() -> {
                try {
                    result = driver.refresh(state);
                } catch (Throwable error) {
                    failure = error;
                } finally {
                    completed.countDown();
                }
            }, "dex-connector-credential-refresh");
            thread.setDaemon(true);
            thread.start();
        }

        private void awaitCompletion(final Duration timeout) throws InterruptedException {
            completed.await(timeout.toNanos(), TimeUnit.NANOSECONDS);
        }

        /** Waits through interrupts until completion or the deadline; returns whether any interrupt arrived. */
        private boolean awaitCompletionUninterruptibly(final Duration grace) {
            final long deadline = System.nanoTime() + grace.toNanos();
            boolean wasInterrupted = Thread.interrupted();
            while (completed.getCount() > 0) {
                final long remaining = deadline - System.nanoTime();
                if (remaining <= 0) {
                    break;
                }
                try {
                    completed.await(remaining, TimeUnit.NANOSECONDS);
                } catch (InterruptedException interrupted) {
                    wasInterrupted = true;
                }
            }
            return wasInterrupted;
        }

        private boolean isDone() {
            return completed.getCount() == 0;
        }

        private void cancel() {
            thread.interrupt();
        }

        private CredentialRefreshDriver.Result<C> result() throws Exception {
            final Throwable error = failure;
            if (error == null) {
                if (result == null) {
                    throw new IOException("credential refresh returned no result");
                }
                return result;
            }
            if (error instanceof Exception) {
                throw (Exception) error;
            }
            throw new IOException("credential refresh failed");
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
            if ("AUTHORIZING".equals(admission.kind)) {
                finishExchange(admission, "REAUTHORIZATION_REQUIRED");
            } else {
                // An expired refresh without a result may still have rotated the token at the provider.
                markOutcomeUnknown(admission);
            }
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
            hex.append(String.format(java.util.Locale.ROOT, "%02x", part & 0xff));
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
