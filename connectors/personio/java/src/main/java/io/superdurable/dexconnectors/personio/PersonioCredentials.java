// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.oauthtoken.CredentialFieldAccessor;
import io.superdurable.dexconnectors.sdk.projectconfig.*;

import java.util.*;

/** Immutable Personio credential material for static or coordinated project providers. */
public final class PersonioCredentials {
    private final String clientId;
    private final SecretString clientSecret;
    final SecretString accessToken;

    /**
     * Bind the integration credentials. An empty access token requests a first exchange with a
     * refreshing provider.
     */
    public PersonioCredentials(
            String clientId, SecretString clientSecret, SecretString accessToken) {
        this.clientId = Objects.requireNonNull(clientId);
        this.clientSecret = Objects.requireNonNull(clientSecret);
        this.accessToken = Objects.requireNonNull(accessToken);
    }

    /**
     * Resolve stored credentials on each call and persist complete replacements through the Dex
     * project.
     */
    public static RefreshingProjectCredentialProvider<PersonioCredentials> fromProject(
            LoadedProject project, String connectionName) {
        return new RefreshingProjectCredentialProvider<>(
                project.getConnections(),
                new ConnectionKey("personio", connectionName),
                fields -> decode(fields, false),
                PersonioCredentials::encode,
                fields -> decode(fields, true));
    }

    private static ObjectNode encode(PersonioCredentials keys) {
        ObjectNode result = PersonioClient.JSON.createObjectNode();
        for (String name : FIELDS.getCredentialFieldNames())
            result.put(name, FIELDS.readCredentialField(keys, name));
        return result;
    }

    private static PersonioCredentials decode(ObjectNode material, boolean shared) {
        material.fields()
                .forEachRemaining(field -> {
                    if (!FIELDS.getCredentialFieldNames().contains(field.getKey())
                            || !(field.getValue().isTextual() || field.getValue().isNull()))
                        throw new IllegalArgumentException("Invalid Personio credential field");
                });
        String id = material.path("client_id").asText(""),
                secret = material.path("client_secret").asText(""),
                token = material.path("access_token").asText("");
        if (id.isEmpty() || (shared ? token.isEmpty() : secret.isEmpty()))
            throw new IllegalArgumentException("Incomplete Personio credential");
        return new PersonioCredentials(id, new SecretString(secret), new SecretString(token));
    }

    static final CredentialFieldAccessor<PersonioCredentials> FIELDS =
            new CredentialFieldAccessor<PersonioCredentials>() {
                private final Set<String> names =
                        Collections.unmodifiableSet(
                                new HashSet<>(
                                        Arrays.asList(
                                                "client_id", "client_secret", "access_token")));

                public Set<String> getCredentialFieldNames() {
                    return names;
                }

                public String readCredentialField(PersonioCredentials keys, String field) {
                    switch (field) {
                        case "client_id":
                            return keys.clientId;
                        case "client_secret":
                            return keys.clientSecret.reveal();
                        case "access_token":
                            return keys.accessToken.reveal();
                        default:
                            return "";
                    }
                }

                public PersonioCredentials withCredentialFields(
                        PersonioCredentials keys, Map<String, String> replacements) {
                    return new PersonioCredentials(
                            replacements.getOrDefault("client_id", keys.clientId),
                            new SecretString(
                                    replacements.getOrDefault(
                                            "client_secret", keys.clientSecret.reveal())),
                            new SecretString(
                                    replacements.getOrDefault(
                                            "access_token", keys.accessToken.reveal())));
                }

                public String readAuthMethodId(PersonioCredentials keys) {
                    return null;
                }
            };
}
