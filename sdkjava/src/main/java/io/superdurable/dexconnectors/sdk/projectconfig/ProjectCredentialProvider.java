// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CredentialProvider;
import java.io.IOException;
import java.time.Instant;
import java.util.function.Function;

/**
 * Resolves a non-refreshing project credential for one connector connection.
 *
 * <p>The decoder validates private material at the connector boundary. Ordinary entries read
 * current storage on every call. Shared entries use the loaded project's broker and optional
 * shared decoder, without persisting credentials in Dex Flow state.
 *
 * @param <C> the connector-specific credential type
 */
public class ProjectCredentialProvider<C> implements CredentialProvider<C> {
    final ConnectionStore store;
    final ConnectionKey key;
    final SharedConnection shared;
    private final Function<ObjectNode, C> decode;
    private final Function<ObjectNode, C> decodeShared;

    /**
     * Binds one connection with an ordinary decoder.
     *
     * @param store the connection store
     * @param key the connection key
     * @param decode validates and converts private material
     */
    public ProjectCredentialProvider(
            final ConnectionStore store, final ConnectionKey key, final Function<ObjectNode, C> decode) {
        this(store, key, decode, null);
    }

    /**
     * Binds one connection with an ordinary decoder and a shared-credential decoder.
     *
     * @param store the connection store
     * @param key the connection key
     * @param decode validates and converts private material
     * @param decodeShared validates broker material, or {@code null} to use {@code decode}
     */
    public ProjectCredentialProvider(
            final ConnectionStore store, final ConnectionKey key, final Function<ObjectNode, C> decode,
            final Function<ObjectNode, C> decodeShared) {
        if (store == null || decode == null || key == null) {
            throw new IllegalArgumentException("project credential store and decoder are required");
        }
        key.path(store.getScope());
        this.store = store;
        this.key = key;
        this.decode = decode;
        this.decodeShared = decodeShared;
        this.shared = store.sharedConnection(key);
    }

    /**
     * Reads current material; an expired credential requires refresh support.
     *
     * @param call the call identity
     * @return the decoded credential
     * @throws IOException if storage or the broker fails
     */
    @Override
    public C resolve(final Call call) throws IOException {
        validateCall(call);
        if (shared != null) {
            return decodeSharedMaterial(shared.resolve(call.getId().value()));
        }
        final ConnectionStore.LoadedMaterial loaded = store.readCredentialMaterial(key);
        final Instant expiresAt = loaded.getConnection().getExpiresAt();
        if (expiresAt != null && !expiresAt.isAfter(Instant.now())) {
            throw new IllegalArgumentException("expired project credential requires refresh");
        }
        return decodeMaterial(loaded.getMaterial());
    }

    final void validateCall(final Call call) {
        if (!call.getOperation().getConnectorId().equals(key.getConnectorId())
                || !call.getConnection().getName().equals(key.getConnectionName())) {
            throw new IllegalArgumentException("connector call differs from configured project connection");
        }
        call.getOperation().validate();
    }

    final C decodeMaterial(final CredentialMaterial material) {
        try {
            return decode.apply(material.getCredentials());
        } catch (RuntimeException error) {
            throw new IllegalArgumentException("project credential decoding failed");
        }
    }

    final C decodeSharedMaterial(final ObjectNode material) {
        final Function<ObjectNode, C> decoder = decodeShared != null ? decodeShared : decode;
        try {
            return decoder.apply(material);
        } catch (RuntimeException error) {
            throw new IllegalArgumentException("shared project credential decoding failed");
        }
    }
}
