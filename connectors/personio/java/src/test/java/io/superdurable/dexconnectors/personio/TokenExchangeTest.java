// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import static org.junit.jupiter.api.Assertions.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.LoadedProject;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicInteger;

class TokenExchangeTest {
    @Test
    void insufficientScopeNeedsReauthorization() throws Exception {
        QueryAttempt<PersonProfile> attempt =
                runExchange(
                        200,
                        "{\"access_token\":\"sample\",\"expires_in\":3600,\"token_type\":\"Bearer\",\"scope\":\"other\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void invalidClientNeedsReauthorization() throws Exception {
        QueryAttempt<PersonProfile> attempt = runExchange(400, "{\"error\":\"invalid_client\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void unavailableTokenEndpointRetries() throws Exception {
        QueryAttempt<PersonProfile> attempt = runExchange(503, "{}");
        assertTrue(attempt.isRetry());
        assertEquals(FailureKind.TRANSPORT, attempt.getFailure().getKind());
    }

    @Test
    void credentialReadFailureRetriesBeforeDispatch() throws Exception {
        assertTrue(
                readWith(
                                call -> {
                                    throw new IllegalArgumentException("fixture decoding failure");
                                })
                        .isRetry());
    }

    @Test
    void absentTokenSelectsAuthenticationDefect() throws Exception {
        QueryAttempt<PersonProfile> result = readWith(call -> null);
        assertEquals("defect", result.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, result.getFailure().getKind());
    }

    private static QueryAttempt<PersonProfile> readWith(
            CredentialProvider<PersonioCredentials> credentials) throws Exception {
        PersonioClient client =
                new PersonioClient(
                        credentials,
                        "https://api.personio.de/v2",
                        request -> {
                            throw new AssertionError("unexpected dispatch");
                        },
                        1 << 20);
        return client.getPerson().invoke(lookupCall(), new GetPersonInput("123"));
    }

    private QueryAttempt<PersonProfile> runExchange(int status, String body) throws Exception {
        LoadedProject project =
                PersonDirectoryWorker.exampleProject("fixture-id", "fixture-secret");
        AtomicInteger exchanges = new AtomicInteger();
        PersonioClient client =
                new PersonioClient(
                        PersonioCredentials.fromProject(project, "personio"),
                        "https://api.personio.de/v2",
                        request -> {
                            assertEquals("https://api.personio.de/v2/auth/token", request.getUrl());
                            exchanges.incrementAndGet();
                            return ProviderHttpResponse.buffered(
                                    status,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        QueryAttempt<PersonProfile> attempt =
                client.getPerson().invoke(lookupCall(), new GetPersonInput("123"));
        assertEquals(1, exchanges.get());
        String storedStatus =
                project.getConnections()
                        .readConnection(
                                new io.superdurable.dexconnectors.sdk.projectconfig.ConnectionKey(
                                        "personio", "personio"))
                        .getStatus();
        assertEquals(status == 503 ? "READY" : "REAUTHORIZATION_REQUIRED", storedStatus);
        return attempt;
    }

    private static Call lookupCall() {
        return Call.create(
                CallId.of("63166940-5976-4be8-b6ce-dc9c14446f1d"),
                new ConnectionRef("personio", "personio"),
                new OperationRef("personio", "getPerson"),
                null,
                1);
    }
}
