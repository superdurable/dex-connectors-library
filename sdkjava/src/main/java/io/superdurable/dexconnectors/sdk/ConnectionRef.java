// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Objects;

/**
 * Logical credential reference that is safe to persist in a Flow.
 *
 * <p>The provider is the connector ID and the name selects one configured connection. The
 * reference never contains secret material.
 */
@JsonPropertyOrder({"provider", "name"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ConnectionRef {
    private final String provider;
    private final String name;

    /**
     * Creates a connection reference; call {@link #validate()} before use.
     *
     * @param provider the connector ID that owns the connection
     * @param name the stable connection name
     */
    @JsonCreator
    public ConnectionRef(
            @JsonProperty(value = "provider", required = true) final String provider,
            @JsonProperty(value = "name", required = true) final String name) {
        this.provider = provider == null ? "" : provider;
        this.name = name == null ? "" : name;
    }

    /**
     * Requires both logical connection fields.
     *
     * @throws IllegalArgumentException if the provider or name is blank
     */
    public void validate() {
        if (provider.trim().isEmpty() || name.trim().isEmpty()) {
            throw new IllegalArgumentException("connection provider and name are required");
        }
    }

    /**
     * Returns the connector ID that owns the connection.
     *
     * @return the provider
     */
    @JsonProperty("provider")
    public String getProvider() {
        return provider;
    }

    /**
     * Returns the stable connection name.
     *
     * @return the name
     */
    @JsonProperty("name")
    public String getName() {
        return name;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof ConnectionRef)) {
            return false;
        }
        final ConnectionRef that = (ConnectionRef) other;
        return provider.equals(that.provider) && name.equals(that.name);
    }

    @Override
    public int hashCode() {
        return Objects.hash(provider, name);
    }

    @Override
    public String toString() {
        return provider + "/" + name;
    }
}
