// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.time.Duration;
import java.time.Instant;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Revision-aware broker cache of one immutable shared connection selection.
 *
 * <p>An unexpired credential is reused until it becomes stale; a broker outage defers rereads for
 * ten seconds while an unexpired credential remains usable. Thread-safe.
 */
public final class SharedConnection {
    private static final int MAXIMUM_TRACKED_CALLS = 4096;
    private static final Duration UNAVAILABLE_BACKOFF = Duration.ofSeconds(10);

    private final String sharedConnectionId;
    private final CredentialBrokerClient broker;
    private final Map<String, IssuedRevision> issuedByCall = new LinkedHashMap<>();
    private volatile BrokerCredential credential;
    private volatile Instant nextReadAfter = Instant.MIN;

    /**
     * Binds one shared connection to its broker.
     *
     * @param sharedConnectionId the shared connection ID
     * @param broker the broker client
     */
    public SharedConnection(final String sharedConnectionId, final CredentialBrokerClient broker) {
        if (sharedConnectionId == null || sharedConnectionId.isEmpty() || broker == null) {
            throw new IllegalArgumentException("shared connection ID and broker are required");
        }
        this.sharedConnectionId = sharedConnectionId;
        this.broker = broker;
    }

    /**
     * Returns the shared connection ID.
     *
     * @return the ID
     */
    public String getSharedConnectionId() {
        return sharedConnectionId;
    }

    /**
     * Returns an unexpired credential, rereading stale material in one caller.
     *
     * @param callId the Call ID that receives the credential, or empty
     * @return a copy of the credential body
     * @throws IOException if the broker is unavailable and no unexpired credential is cached
     */
    public ObjectNode resolve(final String callId) throws IOException {
        Instant now = Instant.now();
        BrokerCredential current = credential;
        if (current != null && !isExpired(current, now) && now.isBefore(nextReadAfter)) {
            recordIssued(callId, current.getRevision(), now);
            return current.getCredential();
        }
        if (current != null && !isStale(current, now)) {
            recordIssued(callId, current.getRevision(), now);
            return current.getCredential();
        }
        synchronized (this) {
            current = credential;
            now = Instant.now();
            if (current == null || (isStale(current, now) && !now.isBefore(nextReadAfter))) {
                try {
                    current = broker.read(sharedConnectionId, 0);
                    credential = current;
                } catch (CredentialBrokerUnavailableException error) {
                    nextReadAfter = now.plus(UNAVAILABLE_BACKOFF);
                    if (current == null || isExpired(current, now)) {
                        throw error;
                    }
                }
            }
            if (current == null || isExpired(current, now)) {
                throw new CredentialBrokerUnavailableException("shared credential is expired", null);
            }
            recordIssued(callId, current.getRevision(), now);
            return current.getCredential();
        }
    }

    /**
     * Forces a broker refresh of the revision this provider call received.
     *
     * @param callId the Call ID whose credential the provider rejected
     * @return a copy of the newer credential body
     * @throws RejectedCredentialNotRefreshedException if the broker returns the rejected revision
     * @throws IOException if the broker is unavailable
     */
    public synchronized ObjectNode resolveAfterRejection(final String callId) throws IOException {
        final Instant now = Instant.now();
        final IssuedRevision issued;
        synchronized (issuedByCall) {
            issued = issuedByCall.get(callId);
        }
        final BrokerCredential cached = credential;
        final long rejectedRevision = issued != null ? issued.revision : (cached != null ? cached.getRevision() : 0);
        final BrokerCredential current;
        if (cached != null && cached.getRevision() != rejectedRevision) {
            current = cached;
        } else {
            current = broker.read(sharedConnectionId, rejectedRevision);
            credential = current;
        }
        if (current.getRevision() == rejectedRevision) {
            throw new RejectedCredentialNotRefreshedException("the rejected credential was not refreshed");
        }
        recordIssued(callId, current.getRevision(), now);
        return current.getCredential();
    }

    private void recordIssued(final String callId, final long revision, final Instant now) {
        if (callId == null || callId.isEmpty()) {
            return;
        }
        synchronized (issuedByCall) {
            issuedByCall.put(callId, new IssuedRevision(revision, now));
            if (issuedByCall.size() > MAXIMUM_TRACKED_CALLS) {
                final Instant cutoff = now.minus(Duration.ofMinutes(10));
                issuedByCall.values().removeIf(value -> !value.issuedAt.isAfter(cutoff));
                if (issuedByCall.size() > MAXIMUM_TRACKED_CALLS) {
                    final Iterator<String> oldest = issuedByCall.keySet().iterator();
                    oldest.next();
                    oldest.remove();
                }
            }
        }
    }

    private static boolean isExpired(final BrokerCredential value, final Instant now) {
        return value.getExpiresAt() != null && !value.getExpiresAt().toInstant().isAfter(now);
    }

    private static boolean isStale(final BrokerCredential value, final Instant now) {
        if (value.getExpiresAt() == null) {
            return Duration.between(value.getReadAt(), now).compareTo(Duration.ofMinutes(15)) >= 0;
        }
        final Instant expiresAt = value.getExpiresAt().toInstant();
        final Instant issuedAt = value.getIssuedAt() != null ? value.getIssuedAt().toInstant() : value.getReadAt();
        final Duration lifetime = Duration.between(issuedAt, expiresAt);
        Duration skew = Duration.ZERO;
        if (!lifetime.isNegative() && !lifetime.isZero()) {
            skew = lifetime.dividedBy(4).compareTo(Duration.ofMinutes(5)) < 0 ? lifetime.dividedBy(4) : Duration.ofMinutes(5);
        }
        return !now.isBefore(expiresAt.minus(skew));
    }

    private static final class IssuedRevision {
        private final long revision;
        private final Instant issuedAt;

        private IssuedRevision(final long revision, final Instant issuedAt) {
            this.revision = revision;
            this.issuedAt = issuedAt;
        }
    }
}
