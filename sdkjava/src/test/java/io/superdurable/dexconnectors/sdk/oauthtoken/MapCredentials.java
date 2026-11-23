// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;

/** Immutable test credentials keyed by manifest field name. */
final class MapCredentials {
    final Map<String, String> values;
    final String authMethodId;

    MapCredentials(final Map<String, String> values, final String authMethodId) {
        this.values = Collections.unmodifiableMap(new LinkedHashMap<>(values));
        this.authMethodId = authMethodId;
    }

    static CredentialFieldAccessor<MapCredentials> accessor(final Set<String> fieldNames) {
        return new CredentialFieldAccessor<MapCredentials>() {
            @Override
            public Set<String> getCredentialFieldNames() {
                return fieldNames;
            }

            @Override
            public String readCredentialField(final MapCredentials credentials, final String fieldName) {
                final String value = credentials.values.get(fieldName);
                return value == null ? "" : value;
            }

            @Override
            public MapCredentials withCredentialFields(final MapCredentials credentials, final Map<String, String> values) {
                final Map<String, String> updated = new LinkedHashMap<>(credentials.values);
                updated.putAll(values);
                return new MapCredentials(updated, credentials.authMethodId);
            }

            @Override
            public String readAuthMethodId(final MapCredentials credentials) {
                return credentials.authMethodId;
            }
        };
    }
}
