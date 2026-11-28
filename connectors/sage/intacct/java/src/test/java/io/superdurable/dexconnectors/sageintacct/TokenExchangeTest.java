// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

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
        QueryAttempt<InvoiceSummary> attempt = runExchange(400, "{\"error\":\"invalid_grant\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void invalidClientNeedsReauthorization() throws Exception {
        QueryAttempt<InvoiceSummary> attempt = runExchange(400, "{\"error\":\"invalid_client\"}");
        assertEquals("defect", attempt.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, attempt.getFailure().getKind());
    }

    @Test
    void failedRotatingTokenExchangeRequiresNewAuthorization() throws Exception {
        QueryAttempt<InvoiceSummary> attempt = runExchange(503, "{}");
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
        QueryAttempt<InvoiceSummary> result = readWith(call -> null);
        assertEquals("defect", result.getBranch());
        assertEquals(FailureKind.AUTHENTICATION, result.getFailure().getKind());
    }

    private static QueryAttempt<InvoiceSummary> readWith(
            CredentialProvider<SageIntacctCredentials> credentials) throws Exception {
        SageIntacctClient client =
                new SageIntacctClient(
                        credentials,
                        "https://tenant.example.com",
                        request -> {
                            throw new AssertionError("unexpected dispatch");
                        },
                        1 << 20);
        return client.getInvoice()
                .invoke(lookupCall(), new GetInvoiceInput("0123456789abcdef0123456789abcdef"));
    }

    private QueryAttempt<InvoiceSummary> runExchange(int status, String body) throws Exception {
        LoadedProject project =
                SageIntacctTestProject.exampleProject("fixture-id", "fixture-secret");
        AtomicInteger exchanges = new AtomicInteger();
        SageIntacctClient client =
                new SageIntacctClient(
                        SageIntacctCredentials.fromProject(project, "sage-intacct"),
                        "https://tenant.example.com",
                        request -> {
                            assertEquals(
                                    "https://api.intacct.com/ia/api/v1/oauth2/token",
                                    request.getUrl());
                            exchanges.incrementAndGet();
                            return ProviderHttpResponse.buffered(
                                    status,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        QueryAttempt<InvoiceSummary> attempt =
                client.getInvoice()
                        .invoke(
                                lookupCall(),
                                new GetInvoiceInput("0123456789abcdef0123456789abcdef"));
        assertEquals(1, exchanges.get());
        String storedStatus =
                project.getConnections()
                        .readConnection(
                                new io.superdurable.dexconnectors.sdk.projectconfig.ConnectionKey(
                                        "sage-intacct", "sage-intacct"))
                        .getStatus();
        assertEquals("REAUTHORIZATION_REQUIRED", storedStatus);
        return attempt;
    }

    private static Call lookupCall() {
        return Call.create(
                CallId.of("63166940-5976-4be8-b6ce-dc9c14446f1d"),
                new ConnectionRef("sage-intacct", "sage-intacct"),
                new OperationRef("sage-intacct", "getInvoice"),
                null,
                1);
    }
}
