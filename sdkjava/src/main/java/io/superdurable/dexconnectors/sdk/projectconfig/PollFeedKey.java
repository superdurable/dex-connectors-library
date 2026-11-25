// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Objects;

/**
 * One poll cursor shared by several Trigger bindings of one connection, such as one change feed.
 */
public final class PollFeedKey implements PollCursorIdentity {
    private final String connectorId;
    private final String connectionName;
    private final String feedName;

    /**
     * Creates a feed key; {@link #path(Scope)} validates it.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application connection the feed reads
     * @param feedName the connector's stable lower-camel feed name
     */
    public PollFeedKey(final String connectorId, final String connectionName, final String feedName) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.connectionName = connectionName == null ? "" : connectionName;
        this.feedName = feedName == null ? "" : feedName;
    }

    /**
     * Returns the head key {@code <scope>/poll-feeds/<connector>/<b64url(connection)>/<feed>/head}.
     *
     * @param scope the project scope
     * @return the head key
     * @throws IllegalArgumentException if the identity is invalid
     */
    @Override
    public String path(final Scope scope) {
        PollCursorNames.validate(connectorId, feedName, connectionName);
        return scope.prefix() + "/poll-feeds/" + connectorId + "/" + ConnectionKey.base64Url(connectionName) + "/"
                + feedName + "/head";
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof PollFeedKey)) {
            return false;
        }
        final PollFeedKey that = (PollFeedKey) other;
        return connectorId.equals(that.connectorId) && connectionName.equals(that.connectionName)
                && feedName.equals(that.feedName);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connectorId, connectionName, feedName);
    }
}
