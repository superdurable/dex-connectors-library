// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import java.util.Map;
import java.util.Set;

/**
 * Reads and replaces manifest-named fields of a connector's immutable credential type.
 *
 * <p>Generated connector code implements it so {@link ManifestCredentialRefreshDriver} can work
 * with any credential class. Field names are the manifest {@code spec.auth} credential names.
 * Implementations wrap secret fields in their own secret type and never log values.
 *
 * @param <C> the connector-specific credential type
 */
public interface CredentialFieldAccessor<C> {
    /**
     * Returns every manifest credential name this credential type stores.
     *
     * @return the field names
     */
    Set<String> getCredentialFieldNames();

    /**
     * Reads one field.
     *
     * @param credentials the credentials
     * @param fieldName a name from {@link #getCredentialFieldNames()}
     * @return the revealed value, or empty when unset
     */
    String readCredentialField(C credentials, String fieldName);

    /**
     * Returns a copy with the given fields replaced and every other field unchanged.
     *
     * @param credentials the current credentials
     * @param values the replacement values by field name
     * @return the complete replacement credentials
     */
    C withCredentialFields(C credentials, Map<String, String> values);

    /**
     * Returns the stored authorization method of a multi-method manifest.
     *
     * @param credentials the credentials
     * @return the method ID, empty for the manifest default, or {@code null} when the credential type
     *     carries no method ID
     */
    String readAuthMethodId(C credentials);
}
