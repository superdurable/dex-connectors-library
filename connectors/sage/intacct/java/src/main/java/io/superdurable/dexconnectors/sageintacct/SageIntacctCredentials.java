// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.oauthtoken.CredentialFieldAccessor;
import io.superdurable.dexconnectors.sdk.projectconfig.*;

import java.util.*;

/** Immutable SageIntacct credential material for static or coordinated project providers. */
public final class SageIntacctCredentials {
    private final String clientId;
    private final SecretString clientSecret;
    final SecretString accessToken;
    private final SecretString refreshToken;

    /** Bind tenant client credentials and the access and refresh tokens returned by OAuth. */
    public SageIntacctCredentials(
            String clientId,
            SecretString clientSecret,
            SecretString accessToken,
            SecretString refreshToken) {
        this.clientId = Objects.requireNonNull(clientId);
        this.clientSecret = Objects.requireNonNull(clientSecret);
        this.accessToken = Objects.requireNonNull(accessToken);
        this.refreshToken = Objects.requireNonNull(refreshToken);
    }

    /**
     * Resolve stored credentials on each call and persist complete replacements through the Dex
     * project.
     */
    public static RefreshingProjectCredentialProvider<SageIntacctCredentials> fromProject(
            LoadedProject project, String connectionName) {
        return new RefreshingProjectCredentialProvider<>(
                project.getConnections(),
                new ConnectionKey("sage-intacct", connectionName),
                fields -> decode(fields, false),
                SageIntacctCredentials::encode,
                fields -> decode(fields, true));
    }

    private static ObjectNode encode(SageIntacctCredentials keys) {
        ObjectNode result = SageIntacctClient.JSON.createObjectNode();
        for (String name : FIELDS.getCredentialFieldNames())
            result.put(name, FIELDS.readCredentialField(keys, name));
        return result;
    }

    private static SageIntacctCredentials decode(ObjectNode material, boolean shared) {
        material.fields()
                .forEachRemaining(field -> {
                    if (!FIELDS.getCredentialFieldNames().contains(field.getKey())
                            || !(field.getValue().isTextual() || field.getValue().isNull()))
                        throw new IllegalArgumentException("Invalid SageIntacct credential field");
                });
        String refresh = material.path("refresh_token").asText("");
        String id = material.path("oauth_client_id").asText(""),
                secret = material.path("oauth_client_secret").asText(""),
                token = material.path("access_token").asText("");
        if (id.isEmpty() || token.isEmpty() || (!shared && (secret.isEmpty() || refresh.isEmpty())))
            throw new IllegalArgumentException("Incomplete SageIntacct credential");
        return new SageIntacctCredentials(
                id, new SecretString(secret), new SecretString(token), new SecretString(refresh));
    }

    static final CredentialFieldAccessor<SageIntacctCredentials> FIELDS =
            new CredentialFieldAccessor<SageIntacctCredentials>() {
                private final Set<String> names =
                        Collections.unmodifiableSet(
                                new HashSet<>(
                                        Arrays.asList(
                                                "oauth_client_id",
                                                "oauth_client_secret",
                                                "access_token",
                                                "refresh_token")));

                public Set<String> getCredentialFieldNames() {
                    return names;
                }

                public String readCredentialField(SageIntacctCredentials keys, String field) {
                    switch (field) {
                        case "oauth_client_id":
                            return keys.clientId;
                        case "oauth_client_secret":
                            return keys.clientSecret.reveal();
                        case "refresh_token":
                            return keys.refreshToken.reveal();
                        case "access_token":
                            return keys.accessToken.reveal();
                        default:
                            return "";
                    }
                }

                public SageIntacctCredentials withCredentialFields(
                        SageIntacctCredentials keys, Map<String, String> replacements) {
                    return new SageIntacctCredentials(
                            replacements.getOrDefault("oauth_client_id", keys.clientId),
                            new SecretString(
                                    replacements.getOrDefault(
                                            "oauth_client_secret", keys.clientSecret.reveal())),
                            new SecretString(
                                    replacements.getOrDefault(
                                            "access_token", keys.accessToken.reveal())),
                            new SecretString(
                                    replacements.getOrDefault(
                                            "refresh_token", keys.refreshToken.reveal())));
                }

                public String readAuthMethodId(SageIntacctCredentials keys) {
                    return null;
                }
            };
}
