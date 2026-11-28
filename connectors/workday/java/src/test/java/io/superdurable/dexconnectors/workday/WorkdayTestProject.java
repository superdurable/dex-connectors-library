// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;

import java.io.IOException;

final class WorkdayTestProject {
    static LoadedProject exampleProject(String id, String secret) throws IOException {
        if (id.isEmpty() || secret.isEmpty())
            throw new IllegalArgumentException("Workday integration credentials are required");
        InMemoryObjectStore objects = new InMemoryObjectStore();
        Scope scope = new Scope("workday-directory", "preview");
        ObjectMapper json = new ObjectMapper();
        ObjectNode credentials =
                json.createObjectNode()
                        .put("oauth_client_id", id)
                        .put("oauth_client_secret", secret)
                        .put("access_token", "expired")
                        .put("refresh_token", "test-refresh");
        ConnectionStore connections = new ConnectionStore(objects, scope);
        connections.replaceCredential(
                new ConnectionKey("workday", "workday"),
                0,
                new CredentialMaterial(
                        credentials,
                        io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp.fromWire(
                                "2000-01-01T00:00:00Z"),
                        "oauth2"));
        ConfigurationStore configs = new ConfigurationStore(objects, scope);
        ConnectionConfiguration entry =
                new ConnectionConfiguration(
                        "workday",
                        "workday",
                        "github.com/superdurable/dex-connectors-library/connectors/workday/go",
                        "workday",
                        json.createObjectNode()
                                .put("baseUrl", "https://tenant.example.com/api/common/v1/demo")
                                .put("tokenEndpoint", "https://tenant.example.com/token")
                                .put(
                                        "authorizationEndpoint",
                                        "https://tenant.example.com/authorize")
                                .set("scopes", json.createArrayNode().add("Staffing")));
        Configuration written =
                configs.writeConfiguration(
                                0, Configuration.newBuilder(scope, 1).addConnection(entry).build())
                        .getConfiguration();
        return ProjectConfigurationLoader.loadProjectFromObjectStore(
                objects, scope, configs.freezeConfiguration(written.getRevision()), null);
    }
}
