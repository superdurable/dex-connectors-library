// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.io.IOException;
import java.time.Clock;
import java.time.format.DateTimeFormatterBuilder;
import java.time.format.DateTimeFormatter;
import java.time.temporal.ChronoField;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.function.Function;

/** Hacker News feed snapshots and their operation-specific Dex Query Step. */
public final class StoryFeed {
    private static final DateTimeFormatter UTC_TIME = new DateTimeFormatterBuilder()
            .appendPattern("uuuu-MM-dd'T'HH:mm:ss").appendFraction(ChronoField.NANO_OF_SECOND, 0, 9, true)
            .appendLiteral('Z').toFormatter().withZone(ZoneOffset.UTC);

    private StoryFeed() {}

    /** Feed and limit for one public snapshot. */
    @JsonInclude(JsonInclude.Include.NON_DEFAULT)
    public static final class ListStoryIDsInput {
        /** Feed name. Empty selects top. */
        public final String feed;
        /** Maximum IDs to return, from one to 500. Zero selects fifty. */
        public final long limit;
        /** Select a feed and result limit. */
        @JsonCreator public ListStoryIDsInput(@JsonProperty("feed") String feed, @JsonProperty("limit") long limit) {
            this.feed = feed == null ? "" : feed;
            this.limit = limit;
        }
    }

    /** Immutable snapshot in provider order with its UTC observation time. */
    public static final class StoryIDs {
        /** First IDs in feed order, bounded by the selected limit. */
        public final List<Long> ids;
        /** Whether the feed contained more IDs than the limit. */
        public final boolean hasMore;
        /** UTC RFC 3339 timestamp with up to nanosecond precision. */
        public final String observedAt;
        /** Restore a feed snapshot from Dex. */
        @JsonCreator public StoryIDs(@JsonProperty("ids") List<Long> ids, @JsonProperty("hasMore") boolean hasMore,
                                     @JsonProperty("observedAt") String observedAt) {
            this.ids = Collections.unmodifiableList(new ArrayList<>(ids));
            this.hasMore = hasMore;
            this.observedAt = observedAt;
        }
    }

    /** Typed input for the Step following the feed query. */
    public static final class StoryFeedResult extends QueryResult<StoryIDs> {
        /** Restore a branch, result, receipt and optional failure from Dex. */
        @JsonCreator public StoryFeedResult(@JsonProperty("branch") String branch, @JsonProperty("value") StoryIDs value,
                                            @JsonProperty("receipt") Receipt receipt, @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build a Step that reads a feed using the application's logical Hacker News connection. */
    public static <I> QueryStep.ConfigBuilder<I, ListStoryIDsInput, StoryIDs, StoryFeedResult> newListStoryIDsStep(
            String stepType, Class<I> inputType, StepAnnotations annotations, Query<ListStoryIDsInput, StoryIDs> operation,
            ConnectionRef connection, Function<I, ListStoryIDsInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider())) throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(stepType, inputType, annotations, operation, connection, mapInput, StoryFeedResult::new);
    }

    /** Read a bounded Firebase feed on each Execute attempt. */
    public static final class ListStoryIDsOperation extends PublicApiQuery<ListStoryIDsInput, StoryIDs> {
        /** Use the public Firebase endpoint and the manifest timeout and size limit. */
        public ListStoryIDsOperation() { super("listStoryIDs", new StoryIDs(Collections.emptyList(), false, "0001-01-01T00:00:00Z")); }

        /** Bind a Firebase v0 base URL, configured transport, positive byte limit and observation clock. */
        public ListStoryIDsOperation(String baseUrl, ProviderHttpTransport transport, int maxResponseBytes, Clock clock) {
            super("listStoryIDs", new StoryIDs(Collections.emptyList(), false, "0001-01-01T00:00:00Z"), baseUrl, transport, maxResponseBytes, clock);
        }

        @Override public QueryAttempt<StoryIDs> invoke(Call call, ListStoryIDsInput input) throws InterruptedException {
            String feed = input.feed.isEmpty() ? "top" : input.feed;
            long limit = input.limit == 0 ? 50 : input.limit;
            if (!feed.matches("top|best|new|ask|show")) return failed("defect", FailureKind.VALIDATION, "feed must be top, best, new, ask, or show");
            if (limit < 1 || limit > 500) return failed("defect", FailureKind.VALIDATION, "limit must be from 1 through 500");
            return read("/" + feed + "stories.json", body -> {
                JsonNode feedIDs;
                try { feedIDs = PublicApiQuery.JSON.readTree(body); }
                catch (IOException e) { feedIDs = null; }
                if (feedIDs == null || !feedIDs.isArray()) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned an invalid feed");
                List<Long> ids = new ArrayList<>((int) Math.min(limit, feedIDs.size()));
                for (JsonNode id : feedIDs) {
                    if (!id.isIntegralNumber() || !id.canConvertToLong() || id.longValue() <= 0)
                        return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned an invalid item ID");
                    if (ids.size() < limit) ids.add(id.longValue());
                }
                return QueryAttempt.branch("listed", new StoryIDs(ids, feedIDs.size() > limit, UTC_TIME.format(clock.instant())), Receipt.empty());
            });
        }
    }
}
