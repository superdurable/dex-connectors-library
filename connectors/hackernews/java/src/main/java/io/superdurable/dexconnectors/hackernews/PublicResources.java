// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import com.fasterxml.jackson.annotation.*;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.type.LogicalType;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.util.*;
import java.util.function.Function;

/** Public Firebase items and user profiles with operation-specific Dex Query Steps. */
public final class PublicResources {
    private PublicResources() {}
    private static final ObjectMapper PROVIDER_JSON = providerMapper();

    private static ObjectMapper providerMapper() {
        ObjectMapper mapper = FirebaseQuery.JSON.copy()
                .disable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, DeserializationFeature.ACCEPT_FLOAT_AS_INT)
                .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS);
        for (CoercionInputShape shape : Arrays.asList(CoercionInputShape.Integer, CoercionInputShape.Float, CoercionInputShape.Boolean))
            mapper.coercionConfigFor(LogicalType.Textual).setCoercion(shape, CoercionAction.Fail);
        return mapper;
    }

    /** Identify one public Hacker News item. */
    public static final class GetItemInput {
        /** Public item ID. */
        public final long id;
        /** Bind the requested ID. */
        @JsonCreator public GetItemInput(@JsonProperty("id") long id) { this.id = id; }
    }

    /** Public fields returned by the Hacker News item Query. */
    public static final class Item {
        /** Public item ID. */
        public final long id;
        /** Story, comment, job, poll, or pollopt. */
        public final String type;
        /** Public author username. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String by;
        /** Creation time in Unix seconds. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long time;
        /** Untrusted provider title. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String title;
        /** Untrusted submitted HTML. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String text;
        /** External URL submitted with the item. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String url;
        /** Canonical Hacker News discussion permalink. */
        public final String discussionUrl;
        /** Public story score. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long score;
        /** Comment count. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long descendants;
        /** Parent item ID. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long parent;
        /** Child IDs in provider order. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final List<Long> kids;
        /** Whether the item is deleted. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final boolean deleted;
        /** Whether the item is moderated. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final boolean dead;

        /** Restore the public wire representation of this item. */
        @JsonCreator public Item(@JsonProperty("id") long id,
                @JsonProperty("type") String type,
                @JsonProperty("by") String by,
                @JsonProperty("time") long time,
                @JsonProperty("title") String title,
                @JsonProperty("text") String text,
                @JsonProperty("url") String url,
                @JsonProperty("discussionUrl") String discussionUrl,
                @JsonProperty("score") long score,
                @JsonProperty("descendants") long descendants,
                @JsonProperty("parent") long parent,
                @JsonProperty("kids") List<Long> kids,
                @JsonProperty("deleted") boolean deleted,
                @JsonProperty("dead") boolean dead) {
            this.id = id;
            this.type = type == null ? "" : type;
            this.by = by == null ? "" : by;
            this.time = time;
            this.title = title == null ? "" : title;
            this.text = text == null ? "" : text;
            this.url = url == null ? "" : url;
            this.discussionUrl = discussionUrl == null ? "" : discussionUrl;
            this.score = score;
            this.descendants = descendants;
            this.parent = parent;
            this.kids = kids == null ? Collections.emptyList() : Collections.unmodifiableList(new ArrayList<>(kids));
            this.deleted = deleted;
            this.dead = dead;
        }

        private static final Item EMPTY = new Item(0, "", "", 0, "", "", "", "", 0, 0, 0, Collections.emptyList(), false, false);
    }

    /** Typed Step result for a public item Query. */
    public static final class ItemResult extends QueryResult<Item> {
        /** Restore the branch, output, receipt and optional failure from Dex. */
        @JsonCreator public ItemResult(@JsonProperty("branch") String branch, @JsonProperty("value") Item value,
                @JsonProperty("receipt") Receipt receipt, @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build the Step that reads one item with the application's registered connection. */
    public static <I> QueryStep.ConfigBuilder<I, GetItemInput, Item, ItemResult> newGetItemStep(
            String stepType, Class<I> inputType, StepAnnotations annotations, Query<GetItemInput, Item> operation,
            ConnectionRef connection, Function<I, GetItemInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider())) throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(stepType, inputType, annotations, operation, connection, mapInput, ItemResult::new);
    }

    /** Read one bounded public item with each Execute attempt. */
    public static final class GetItemOperation extends FirebaseQuery<GetItemInput, Item> {
        /** Use the public Firebase endpoint and the manifest timeout and size limit. */
        public GetItemOperation() { super("getItem", Item.EMPTY); }
        /** Bind a Firebase endpoint, bounded transport and clock for Retry-After. */
        public GetItemOperation(String baseUrl, ProviderHttpTransport transport, int maxResponseBytes, Clock clock) {
            super("getItem", Item.EMPTY, baseUrl, transport, maxResponseBytes, clock);
        }

        @Override public QueryAttempt<Item> invoke(Call call, GetItemInput input) throws InterruptedException {
            if (input.id <= 0) return failed("defect", FailureKind.VALIDATION, "item ID must be positive");
            return read("/item/" + input.id + ".json", body -> {
                Item item;
                try { item = PROVIDER_JSON.readValue(body, Item.class); }
                catch (IOException e) { return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned malformed item JSON"); }
                if (item == null || item.deleted || item.dead) return failed("notFound", FailureKind.NOT_FOUND, "Hacker News item is deleted or dead");
                if (item.id != input.id) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned a different item ID");
                if (!item.type.matches("story|comment|poll|pollopt|job")) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned an unknown item type");
                if (invalidIDs(item.kids)) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned an invalid child ID");
                Item found = new Item(item.id, item.type, item.by, item.time, item.title, item.text, item.url,
                        "https://news.ycombinator.com/item?id=" + item.id, item.score, item.descendants, item.parent, item.kids, false, false);
                return QueryAttempt.branch("found", found, Receipt.empty());
            });
        }
    }

    /** Identify one public Hacker News user. */
    public static final class GetUserInput {
        /** Case-sensitive username. */
        public final String id;
        /** Bind the requested ID. */
        @JsonCreator public GetUserInput(@JsonProperty("id") String id) { this.id = id; }
    }

    /** Public fields returned by the Hacker News user Query. */
    public static final class User {
        /** Case-sensitive username. */
        public final String id;
        /** Account creation in Unix seconds. */
        public final long created;
        /** Public karma. */
        public final long karma;
        /** Untrusted profile HTML. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String about;
        /** Public submitted item IDs. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final List<Long> submitted;
        /** Canonical Hacker News profile permalink. */
        public final String profileUrl;

        /** Restore the public wire representation of this user. */
        @JsonCreator public User(@JsonProperty("id") String id,
                @JsonProperty("created") long created,
                @JsonProperty("karma") long karma,
                @JsonProperty("about") String about,
                @JsonProperty("submitted") List<Long> submitted,
                @JsonProperty("profileUrl") String profileUrl) {
            this.id = id == null ? "" : id;
            this.created = created;
            this.karma = karma;
            this.about = about == null ? "" : about;
            this.submitted = submitted == null ? Collections.emptyList() : Collections.unmodifiableList(new ArrayList<>(submitted));
            this.profileUrl = profileUrl == null ? "" : profileUrl;
        }

        private static final User EMPTY = new User("", 0, 0, "", Collections.emptyList(), "");
    }

    /** Typed Step result for a public user Query. */
    public static final class UserResult extends QueryResult<User> {
        /** Restore the branch, output, receipt and optional failure from Dex. */
        @JsonCreator public UserResult(@JsonProperty("branch") String branch, @JsonProperty("value") User value,
                @JsonProperty("receipt") Receipt receipt, @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build the Step that reads one user with the application's registered connection. */
    public static <I> QueryStep.ConfigBuilder<I, GetUserInput, User, UserResult> newGetUserStep(
            String stepType, Class<I> inputType, StepAnnotations annotations, Query<GetUserInput, User> operation,
            ConnectionRef connection, Function<I, GetUserInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider())) throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(stepType, inputType, annotations, operation, connection, mapInput, UserResult::new);
    }

    /** Read one bounded public user with each Execute attempt. */
    public static final class GetUserOperation extends FirebaseQuery<GetUserInput, User> {
        /** Use the public Firebase endpoint and the manifest timeout and size limit. */
        public GetUserOperation() { super("getUser", User.EMPTY); }
        /** Bind a Firebase endpoint, bounded transport and clock for Retry-After. */
        public GetUserOperation(String baseUrl, ProviderHttpTransport transport, int maxResponseBytes, Clock clock) {
            super("getUser", User.EMPTY, baseUrl, transport, maxResponseBytes, clock);
        }

        @Override public QueryAttempt<User> invoke(Call call, GetUserInput input) throws InterruptedException {
            String id = input.id;
            if (id == null || id.isEmpty() || id.getBytes(StandardCharsets.UTF_8).length > 100 || id.indexOf('/') >= 0 || id.indexOf('?') >= 0 || id.indexOf('#') >= 0)
                return failed("defect", FailureKind.VALIDATION, "username is invalid");
            return read("/user/" + escapeUsername(id, false) + ".json", body -> {
                User user;
                try { user = PROVIDER_JSON.readValue(body, User.class); }
                catch (IOException e) { return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned malformed user JSON"); }
                if (user == null) return failed("notFound", FailureKind.NOT_FOUND, "Hacker News user was not found");
                if (!id.equals(user.id) || user.created < 0 || user.karma < 0) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned invalid user data");
                if (invalidIDs(user.submitted)) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News returned an invalid submitted item ID");
                User found = new User(user.id, user.created, user.karma, user.about, user.submitted,
                        "https://news.ycombinator.com/user?id=" + escapeUsername(id, true));
                return QueryAttempt.branch("found", found, Receipt.empty());
            });
        }
    }

    private static boolean invalidIDs(List<Long> ids) {
        return ids.stream().anyMatch(id -> id == null || id <= 0);
    }

    private static String escapeUsername(String id, boolean query) {
        StringBuilder escaped = new StringBuilder();
        String hex = "0123456789ABCDEF";
        for (byte value : id.getBytes(StandardCharsets.UTF_8)) {
            int b = Byte.toUnsignedInt(value);
            if ((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
                    || "-._~".indexOf(b) >= 0 || (!query && "$&+:=@".indexOf(b) >= 0)) escaped.append((char) b);
            else if (query && b == ' ') escaped.append('+');
            else escaped.append('%').append(hex.charAt(b >>> 4)).append(hex.charAt(b & 15));
        }
        return escaped.toString();
    }
}
