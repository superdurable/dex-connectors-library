// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import static org.junit.jupiter.api.Assertions.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.LoadedProject;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicInteger;

class TokenExchangeTest {
    @Test
    void revokedRefreshTokenNeedsReauthorization() throws Exception {
        QueryAttempt<WorkerSummary> attempt = runExchange(400, "{\"error\":\"invalid_grant\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void invalidClientNeedsReauthorization() throws Exception {
        QueryAttempt<WorkerSummary> attempt = runExchange(400, "{\"error\":\"invalid_client\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void failedRotatingTokenExchangeRequiresNewAuthorization() throws Exception {
        QueryAttempt<WorkerSummary> attempt = runExchange(503, "{}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
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
        QueryAttempt<WorkerSummary> result = readWith(call -> null);
        assertEquals("defect", result.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, result.getFailure().getKind());
    }

    private static QueryAttempt<WorkerSummary> readWith(
            CredentialProvider<WorkdayCredentials> credentials) throws Exception {
        WorkdayClient client =
                new WorkdayClient(
                        credentials,
                        "https://tenant.example.com/api/common/v1/demo",
                        "https://tenant.example.com/token",
                        request -> {
                            throw new AssertionError("unexpected dispatch");
                        },
                        1 << 20);
        return client.getWorker()
                .invoke(lookupCall(), new GetWorkerInput("0123456789abcdef0123456789abcdef"));
    }

    private QueryAttempt<WorkerSummary> runExchange(int status, String body) throws Exception {
        LoadedProject project = WorkdayTestProject.exampleProject("fixture-id", "fixture-secret");
        AtomicInteger exchanges = new AtomicInteger();
        WorkdayClient client =
                new WorkdayClient(
                        WorkdayCredentials.fromProject(project, "workday"),
                        "https://tenant.example.com/api/common/v1/demo",
                        "https://tenant.example.com/token",
                        request -> {
                            assertEquals("https://tenant.example.com/token", request.getUrl());
                            exchanges.incrementAndGet();
                            return ProviderHttpResponse.buffered(
                                    status,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        QueryAttempt<WorkerSummary> attempt =
                client.getWorker()
                        .invoke(
                                lookupCall(),
                                new GetWorkerInput("0123456789abcdef0123456789abcdef"));
        assertEquals(1, exchanges.get());
        String storedStatus =
                project.getConnections()
                        .readConnection(
                                new io.superdurable.dexconnectors.sdk.projectconfig.ConnectionKey(
                                        "workday", "workday"))
                        .getStatus();
        assertEquals("REAUTHORIZATION_REQUIRED", storedStatus);
        return attempt;
    }

    private static Call lookupCall() {
        return Call.create(
                CallId.of("63166940-5976-4be8-b6ce-dc9c14446f1d"),
                new ConnectionRef("workday", "workday"),
                new OperationRef("workday", "getWorker"),
                null,
                1);
    }
}
