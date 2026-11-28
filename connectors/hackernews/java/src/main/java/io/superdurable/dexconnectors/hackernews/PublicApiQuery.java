// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.io.IOException;
import java.io.InputStream;
import java.time.Clock;
import java.time.Duration;
import java.util.Objects;
import java.util.function.Function;

abstract class PublicApiQuery<I, O> implements Query<I, O> {
    static final ObjectMapper JSON = new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private final String operation;
    private final O empty;
    private final String baseUrl;
    private final ProviderHttpTransport transport;
    private final int maxResponseBytes;
    final Clock clock;
    private final QueryDefinition definition;

    PublicApiQuery(String operation, O empty) {
        this(operation, empty, "https://hacker-news.firebaseio.com/v0");
    }

    PublicApiQuery(String operation, O empty, String baseUrl) {
        this(operation, empty, baseUrl, new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS), 1 << 20, Clock.systemUTC());
    }

    PublicApiQuery(String operation, O empty, String baseUrl, ProviderHttpTransport transport, int maxResponseBytes, Clock clock) {
        this.operation = operation;
        this.empty = empty;
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        this.transport = Objects.requireNonNull(transport);
        this.clock = Objects.requireNonNull(clock);
        if (maxResponseBytes < 1) throw new IllegalArgumentException("response byte limit must be positive");
        this.maxResponseBytes = maxResponseBytes;
        try (InputStream input = PublicApiQuery.class.getResourceAsStream("/META-INF/dex-connectors/hackernews/operation-definitions.json")) {
            this.definition = JSON.treeToValue(JSON.readTree(Objects.requireNonNull(input)).required(operation), QueryDefinition.class);
            definition.validate();
        } catch (IOException e) { throw new IllegalStateException("invalid Hacker News Query definition", e); }
    }

    @Override public final QueryDefinition definition() { return definition; }

    final QueryAttempt<O> read(String path, Function<byte[], QueryAttempt<O>> decode) throws InterruptedException {
        ProviderHttpRequest request = ProviderHttpRequest.newBuilder("GET", baseUrl + path)
                .header("Accept", "application/json").header("User-Agent", "superdurable-hacker-news-daily/0.1").build();
        try (ProviderHttpResponse response = transport.send(request)) {
            int status = response.getStatusCode();
            if (status == 408 || status == 429 || status >= 500) {
                FailureKind kind = status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY;
                return retry(kind, "Hacker News is temporarily unavailable",
                        ProviderHttp.parseRetryAfter(response.getHeaders().first("Retry-After"), clock.instant()));
            }
            if (status == 404 && (operation.equals("getItem") || operation.equals("getUser") || operation.equals("getArticleThread"))) return failed("notFound", FailureKind.NOT_FOUND, "Hacker News resource was not found");
            if (status != 200) return failed("providerRejected", FailureKind.PROVIDER_REJECTION, "Hacker News rejected the request");
            byte[] body;
            try { body = response.readBoundedBody(maxResponseBytes); }
            catch (BodyTooLargeException e) { return failed("invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Hacker News response exceeded the configured limit"); }
            catch (IOException e) { return retry(FailureKind.AVAILABILITY, "Hacker News response was interrupted", Duration.ZERO); }
            return decode.apply(body);
        } catch (IOException e) { return retry(FailureKind.AVAILABILITY, "Hacker News request failed", Duration.ZERO); }
    }

    final QueryAttempt<O> failed(String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(branch, empty, Receipt.empty(), new Failure(kind, "hacker-news", operation, message));
    }

    private QueryAttempt<O> retry(FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, "hacker-news", operation, message), delay);
    }
}
