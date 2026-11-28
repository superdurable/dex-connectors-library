// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiling;

import static org.junit.jupiter.api.Assertions.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

class TokenCacheTest {
    @Test
    void refreshesAtTheSafetyWindowAndOnRotation() throws Exception {
        AtomicLong elapsed = new AtomicLong();
        AtomicInteger issued = new AtomicInteger();
        AtomicReference<String> secret = new AtomicReference<>("first");
        ProviderHttpTransport server =
                request -> {
                    String body;
                    if (request.getUrl().contains("/auth/")) {
                        int number = issued.incrementAndGet();
                        assertTrue(request.getUrl().contains("app_secret=" + secret.get()));
                        body =
                                "{\"code\":0,\"data\":{\"access_token\":\"token-"
                                        + number
                                        + "\",\"expires_in\":180}}";
                    } else {
                        assertTrue(
                                request.getUrl().endsWith("?access_token=token-" + issued.get()));
                        body = "{\"code\":0,\"data\":{\"data_list\":[]}}";
                    }
                    return ProviderHttpResponse.buffered(
                            200, ProviderHttpHeaders.EMPTY, body.getBytes(StandardCharsets.UTF_8));
                };
        DepartmentDirectory.ListDepartmentsOperation query =
                new DepartmentDirectory.ListDepartmentsOperation(
                        c ->
                                new DepartmentDirectory.Credentials(
                                        new SecretString("account"),
                                        new SecretString(secret.get())),
                        "https://openapi.weiling.cn",
                        server,
                        1024,
                        elapsed::get);
        Call call =
                Call.create(
                        CallId.of("63166940-5976-4be8-b6ce-dc9c14446f1d"),
                        new ConnectionRef("weiling", "weiling"),
                        new OperationRef("weiling", "listDepartments"),
                        null,
                        1);
        DepartmentDirectory.ListDepartmentsInput input =
                new DepartmentDirectory.ListDepartmentsInput();
        assertEquals("listed", query.invoke(call, input).getBranch());
        elapsed.set(TimeUnit.SECONDS.toNanos(60) - 1);
        assertEquals("listed", query.invoke(call, input).getBranch());
        assertEquals(1, issued.get());
        elapsed.incrementAndGet();
        assertEquals("listed", query.invoke(call, input).getBranch());
        assertEquals(2, issued.get());
        secret.set("rotated");
        assertEquals("listed", query.invoke(call, input).getBranch());
        assertEquals(3, issued.get());
    }
}
