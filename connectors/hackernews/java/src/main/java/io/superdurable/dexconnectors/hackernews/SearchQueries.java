// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import com.fasterxml.jackson.annotation.*;
import com.fasterxml.jackson.databind.JavaType;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.*;
import java.util.*;
import java.util.function.Function;

/** Typed queries for the public Hacker News keyword search index. */
public final class SearchQueries {
    private SearchQueries() {}

    /** Maximum Unicode code points in a hit's plain text. */
    public static final int MAX_STORY_TEXT_CHARACTERS = 2000;

    private static final String SEARCH_ORIGIN = "https://hn.algolia.com/api/v1";
    private static final List<String> STORY_TAGS =
            Arrays.asList("story", "show_hn", "ask_hn", "front_page");
    private static final List<String> ITEM_TYPES =
            Arrays.asList("story", "comment", "poll", "pollopt");

    /** Filters for one page of indexed results. */
    public static final class SearchStoriesInput {
        /** Keywords up to 512 UTF-8 bytes. Blank matches all items allowed by the filters. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String query;

        /** Relevance or date. Blank selects relevance. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String sortBy;

        /** Story tags which must all match. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final List<String> tags;

        /** Strict lower creation bound. Omit to include all earlier hits. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final Rfc3339NanoTimestamp createdAfter;

        /** Strict upper creation bound. Omit to include later hits. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final Rfc3339NanoTimestamp createdBefore;

        /** Minimum score. Zero omits the filter. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long minPoints;

        /** Zero-based page within the first 1,000 results. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long page;

        /** Page size from one to 100. Zero selects 20. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long hitsPerPage;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public SearchStoriesInput(
                @JsonProperty("query") String query,
                @JsonProperty("sortBy") String sortBy,
                @JsonProperty("tags") List<String> tags,
                @JsonProperty("createdAfter") Rfc3339NanoTimestamp createdAfter,
                @JsonProperty("createdBefore") Rfc3339NanoTimestamp createdBefore,
                @JsonProperty("minPoints") long minPoints,
                @JsonProperty("page") long page,
                @JsonProperty("hitsPerPage") long hitsPerPage) {
            this.query = query == null ? "" : query;
            this.sortBy = sortBy == null ? "" : sortBy;
            this.tags =
                    tags == null
                            ? Collections.emptyList()
                            : Collections.unmodifiableList(new ArrayList<>(tags));
            this.createdAfter = createdAfter == null || createdAfter.isZero() ? null : createdAfter;
            this.createdBefore =
                    createdBefore == null || createdBefore.isZero() ? null : createdBefore;
            this.minPoints = minPoints;
            this.page = page;
            this.hitsPerPage = hitsPerPage;
        }
    }

    /** One indexed public Hacker News result. */
    public static final class StorySearchHit {
        /** Public Hacker News item ID. */
        public final long id;

        /** Untrusted indexed title. */
        public final String title;

        /** External source URL when indexed. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String url;

        /** Public Hacker News username. */
        public final String author;

        /** Canonical discussion permalink. */
        public final String discussionUrl;

        /** UTC creation time at second precision. */
        public final Rfc3339NanoTimestamp createdAt;

        /** Indexed score. */
        public final long points;

        /** Indexed comment count. */
        public final long commentCount;

        /** Recognized story tags in canonical order. */
        public final List<String> tags;

        /** Plain submission text with paragraph breaks. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String storyText;

        /** Whether the character limit truncated the submission. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final boolean isStoryTextTruncated;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public StorySearchHit(
                @JsonProperty("id") long id,
                @JsonProperty("title") String title,
                @JsonProperty("url") String url,
                @JsonProperty("author") String author,
                @JsonProperty("discussionUrl") String discussionUrl,
                @JsonProperty("createdAt") Rfc3339NanoTimestamp createdAt,
                @JsonProperty("points") long points,
                @JsonProperty("commentCount") long commentCount,
                @JsonProperty("tags") List<String> tags,
                @JsonProperty("storyText") String storyText,
                @JsonProperty("isStoryTextTruncated") boolean isStoryTextTruncated) {
            this.id = id;
            this.title = title == null ? "" : title;
            this.url = url == null ? "" : url;
            this.author = author == null ? "" : author;
            this.discussionUrl = discussionUrl == null ? "" : discussionUrl;
            this.createdAt = createdAt;
            this.points = points;
            this.commentCount = commentCount;
            this.tags =
                    tags == null
                            ? Collections.emptyList()
                            : Collections.unmodifiableList(new ArrayList<>(tags));
            this.storyText = storyText == null ? "" : storyText;
            this.isStoryTextTruncated = isStoryTextTruncated;
        }
    }

    /** Public matches and continuation information for one result page. */
    public static final class StorySearchPage {
        /** Indexed hits in provider order. */
        public final List<StorySearchHit> stories;

        /** Current zero-based page. */
        public final long page;

        /** Number of readable pages. */
        public final long pages;

        /** Estimated total matches. */
        public final long totalHits;

        /** Page size used by this query. */
        public final long hitsPerPage;

        /** Next page number or zero after the last page. */
        public final long nextPage;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public StorySearchPage(
                @JsonProperty("stories") List<StorySearchHit> stories,
                @JsonProperty("page") long page,
                @JsonProperty("pages") long pages,
                @JsonProperty("totalHits") long totalHits,
                @JsonProperty("hitsPerPage") long hitsPerPage,
                @JsonProperty("nextPage") long nextPage) {
            this.stories =
                    stories == null
                            ? Collections.emptyList()
                            : Collections.unmodifiableList(new ArrayList<>(stories));
            this.page = page;
            this.pages = pages;
            this.totalHits = totalHits;
            this.hitsPerPage = hitsPerPage;
            this.nextPage = nextPage;
        }

        private static final StorySearchPage EMPTY =
                new StorySearchPage(Collections.emptyList(), 0, 0, 0, 0, 0);
    }

    /** Typed Dex Step result for a page of indexed story matches. */
    public static final class StorySearchResult extends QueryResult<StorySearchPage> {
        /** Restore the branch, search page, receipt and optional failure. */
        @JsonCreator
        public StorySearchResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") StorySearchPage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build the search Step for the application's named Hacker News connection. */
    public static <I>
            QueryStep.ConfigBuilder<I, SearchStoriesInput, StorySearchPage, StorySearchResult>
                    newSearchStoriesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<SearchStoriesInput, StorySearchPage> operation,
                            ConnectionRef connection,
                            Function<I, SearchStoriesInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider()))
            throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                StorySearchResult::new);
    }

    /** Search one bounded page with each Execute attempt. */
    public static final class SearchStoriesOperation
            extends PublicApiQuery<SearchStoriesInput, StorySearchPage> {
        /** Use the public Search API and manifest request limits. */
        public SearchStoriesOperation() {
            super("searchStories", StorySearchPage.EMPTY, SEARCH_ORIGIN);
        }

        /** Bind a Search endpoint, bounded transport and clock for Retry-After. */
        public SearchStoriesOperation(
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                Clock clock) {
            super(
                    "searchStories",
                    StorySearchPage.EMPTY,
                    baseUrl,
                    transport,
                    maxResponseBytes,
                    clock);
        }

        @Override
        public QueryAttempt<StorySearchPage> invoke(Call call, SearchStoriesInput input)
                throws InterruptedException {
            SearchRequest request;
            try {
                request = requestFor(input);
            } catch (IllegalArgumentException | DateTimeException error) {
                return failed(
                        "defect", FailureKind.VALIDATION, "Hacker News search filters are invalid");
            }
            return read(
                    request.path,
                    body -> {
                        try {
                            IndexedPage<RawStory> page = request.read(body, RawStory.class);
                            List<StorySearchHit> hits = new ArrayList<>(page.hits.size());
                            for (RawStory hit : page.hits) hits.add(hit.decode());
                            return QueryAttempt.branch(
                                    "searched",
                                    new StorySearchPage(
                                            hits,
                                            request.page,
                                            page.pages,
                                            page.totalHits,
                                            request.size,
                                            request.next(page.pages)),
                                    Receipt.empty());
                        } catch (IOException | IllegalArgumentException | DateTimeException error) {
                            return failed(
                                    "invalidResponse",
                                    FailureKind.PROTOCOL,
                                    "Hacker News Search returned an invalid result page");
                        }
                    });
        }
    }

    /** Filters for one page of indexed results. */
    public static final class SearchItemsInput {
        /** Keywords up to 512 UTF-8 bytes. Blank matches all items allowed by the filters. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String query;

        /** Relevance or date. Blank selects relevance. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String sortBy;

        /** Story, comment, poll, or pollopt. Blank searches all four. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String type;

        /** Strict lower creation bound. Omit to include all earlier hits. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final Rfc3339NanoTimestamp createdAfter;

        /** Zero-based page within the first 1,000 results. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long page;

        /** Page size from one to 100. Zero selects 20. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long hitsPerPage;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public SearchItemsInput(
                @JsonProperty("query") String query,
                @JsonProperty("sortBy") String sortBy,
                @JsonProperty("type") String type,
                @JsonProperty("createdAfter") Rfc3339NanoTimestamp createdAfter,
                @JsonProperty("page") long page,
                @JsonProperty("hitsPerPage") long hitsPerPage) {
            this.query = query == null ? "" : query;
            this.sortBy = sortBy == null ? "" : sortBy;
            this.type = type == null ? "" : type;
            this.createdAfter = createdAfter == null || createdAfter.isZero() ? null : createdAfter;
            this.page = page;
            this.hitsPerPage = hitsPerPage;
        }
    }

    /** One indexed public Hacker News result. */
    public static final class ItemSearchHit {
        /** Public Hacker News item ID. */
        public final long id;

        /** Untrusted indexed title. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String title;

        /** External source URL when indexed. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String url;

        /** Public Hacker News username. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String author;

        /** Canonical discussion permalink. */
        public final String discussionUrl;

        /** UTC creation time at second precision. */
        public final Rfc3339NanoTimestamp createdAt;

        /** First indexed item type in the provider tags. */
        public final String type;

        /** Bounded plain story or comment text. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String text;

        /** Direct parent item ID when indexed. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long parentId;

        /** Associated story ID when indexed. */
        @JsonInclude(JsonInclude.Include.NON_DEFAULT)
        public final long storyId;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public ItemSearchHit(
                @JsonProperty("id") long id,
                @JsonProperty("title") String title,
                @JsonProperty("url") String url,
                @JsonProperty("author") String author,
                @JsonProperty("discussionUrl") String discussionUrl,
                @JsonProperty("createdAt") Rfc3339NanoTimestamp createdAt,
                @JsonProperty("type") String type,
                @JsonProperty("text") String text,
                @JsonProperty("parentId") long parentId,
                @JsonProperty("storyId") long storyId) {
            this.id = id;
            this.title = title == null ? "" : title;
            this.url = url == null ? "" : url;
            this.author = author == null ? "" : author;
            this.discussionUrl = discussionUrl == null ? "" : discussionUrl;
            this.createdAt = createdAt;
            this.type = type == null ? "" : type;
            this.text = text == null ? "" : text;
            this.parentId = parentId;
            this.storyId = storyId;
        }
    }

    /** Public matches and continuation information for one result page. */
    public static final class ItemSearchPage {
        /** Indexed hits in provider order. */
        public final List<ItemSearchHit> items;

        /** Current zero-based page. */
        public final long page;

        /** Number of readable pages. */
        public final long pages;

        /** Estimated total matches. */
        public final long totalHits;

        /** Page size used by this query. */
        public final long hitsPerPage;

        /** Next page number or zero after the last page. */
        public final long nextPage;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator
        public ItemSearchPage(
                @JsonProperty("items") List<ItemSearchHit> items,
                @JsonProperty("page") long page,
                @JsonProperty("pages") long pages,
                @JsonProperty("totalHits") long totalHits,
                @JsonProperty("hitsPerPage") long hitsPerPage,
                @JsonProperty("nextPage") long nextPage) {
            this.items =
                    items == null
                            ? Collections.emptyList()
                            : Collections.unmodifiableList(new ArrayList<>(items));
            this.page = page;
            this.pages = pages;
            this.totalHits = totalHits;
            this.hitsPerPage = hitsPerPage;
            this.nextPage = nextPage;
        }

        private static final ItemSearchPage EMPTY =
                new ItemSearchPage(Collections.emptyList(), 0, 0, 0, 0, 0);
    }

    /** Typed Dex Step result for a page of indexed item matches. */
    public static final class ItemSearchResult extends QueryResult<ItemSearchPage> {
        /** Restore the branch, search page, receipt and optional failure. */
        @JsonCreator
        public ItemSearchResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ItemSearchPage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build the search Step for the application's named Hacker News connection. */
    public static <I>
            QueryStep.ConfigBuilder<I, SearchItemsInput, ItemSearchPage, ItemSearchResult>
                    newSearchItemsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<SearchItemsInput, ItemSearchPage> operation,
                            ConnectionRef connection,
                            Function<I, SearchItemsInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider()))
            throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ItemSearchResult::new);
    }

    /** Search one bounded page with each Execute attempt. */
    public static final class SearchItemsOperation
            extends PublicApiQuery<SearchItemsInput, ItemSearchPage> {
        /** Use the public Search API and manifest request limits. */
        public SearchItemsOperation() {
            super("searchItems", ItemSearchPage.EMPTY, SEARCH_ORIGIN);
        }

        /** Bind a Search endpoint, bounded transport and clock for Retry-After. */
        public SearchItemsOperation(
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                Clock clock) {
            super("searchItems", ItemSearchPage.EMPTY, baseUrl, transport, maxResponseBytes, clock);
        }

        @Override
        public QueryAttempt<ItemSearchPage> invoke(Call call, SearchItemsInput input)
                throws InterruptedException {
            SearchRequest request;
            try {
                request = requestFor(input);
            } catch (IllegalArgumentException | DateTimeException error) {
                return failed(
                        "defect", FailureKind.VALIDATION, "Hacker News search filters are invalid");
            }
            return read(
                    request.path,
                    body -> {
                        try {
                            IndexedPage<RawItem> page = request.read(body, RawItem.class);
                            List<ItemSearchHit> hits = new ArrayList<>(page.hits.size());
                            for (RawItem hit : page.hits) hits.add(hit.decode(input.type));
                            return QueryAttempt.branch(
                                    "searched",
                                    new ItemSearchPage(
                                            hits,
                                            request.page,
                                            page.pages,
                                            page.totalHits,
                                            request.size,
                                            request.next(page.pages)),
                                    Receipt.empty());
                        } catch (IOException | IllegalArgumentException | DateTimeException error) {
                            return failed(
                                    "invalidResponse",
                                    FailureKind.PROTOCOL,
                                    "Hacker News Search returned an invalid result page");
                        }
                    });
        }
    }

    private static SearchRequest requestFor(SearchStoriesInput input) {
        Set<String> tags = new LinkedHashSet<>();
        tags.add("story");
        for (String tag : input.tags) {
            if (!STORY_TAGS.contains(tag)) throw new IllegalArgumentException("invalid story tag");
            tags.add(tag);
        }
        if (input.minPoints < 0) throw new IllegalArgumentException("negative score filter");
        List<String> filters = afterFilter(input.createdAfter);
        if (input.createdBefore != null) {
            Instant before = instant(input.createdBefore);
            if (input.createdAfter != null && !before.isAfter(instant(input.createdAfter)))
                throw new IllegalArgumentException("invalid date window");
            filters.add(
                    "created_at_i<" + (before.getEpochSecond() + (before.getNano() > 0 ? 1 : 0)));
        }
        if (input.minPoints > 0) filters.add("points>=" + input.minPoints);
        return new SearchRequest(
                input.query,
                input.sortBy,
                String.join(",", tags),
                filters,
                input.page,
                input.hitsPerPage);
    }

    private static SearchRequest requestFor(SearchItemsInput input) {
        if (!input.type.isEmpty() && !ITEM_TYPES.contains(input.type))
            throw new IllegalArgumentException("invalid item type");
        return new SearchRequest(
                input.query,
                input.sortBy,
                input.type,
                afterFilter(input.createdAfter),
                input.page,
                input.hitsPerPage);
    }

    private static List<String> afterFilter(Rfc3339NanoTimestamp after) {
        List<String> filters = new ArrayList<>();
        if (after != null) filters.add("created_at_i>" + instant(after).getEpochSecond());
        return filters;
    }

    private static Instant instant(Rfc3339NanoTimestamp time) {
        String wire = time.toWire();
        if (wire.endsWith("Z")) return Instant.parse(wire);
        // Go accepts offsets beyond the range of java.time.ZoneOffset.
        int zoneStart = wire.length() - 6;
        int minutes =
                Integer.parseInt(wire.substring(zoneStart + 1, zoneStart + 3)) * 60
                        + Integer.parseInt(wire.substring(zoneStart + 4));
        Instant local = LocalDateTime.parse(wire.substring(0, zoneStart)).toInstant(ZoneOffset.UTC);
        return local.minusSeconds((wire.charAt(zoneStart) == '+' ? 1 : -1) * minutes * 60L);
    }

    private static final class SearchRequest {
        final String path;
        final long page, size;

        SearchRequest(
                String query,
                String sort,
                String tags,
                List<String> filters,
                long page,
                long hitsPerPage) {
            if (query.getBytes(StandardCharsets.UTF_8).length > 512)
                throw new IllegalArgumentException("query is too long");
            if (!sort.isEmpty() && !sort.equals("relevance") && !sort.equals("date"))
                throw new IllegalArgumentException("invalid sort");
            this.page = page;
            this.size = hitsPerPage == 0 ? 20 : hitsPerPage;
            if (size < 1 || size > 100 || page < 0 || page > 999 / size)
                throw new IllegalArgumentException("invalid search page");
            Map<String, String> params = new TreeMap<>();
            params.put("page", Long.toString(page));
            params.put("hitsPerPage", Long.toString(size));
            if (!tags.isEmpty()) params.put("tags", tags);
            if (!filters.isEmpty()) params.put("numericFilters", String.join(",", filters));
            if (!query.isEmpty()) params.put("query", query);
            StringJoiner encoded = new StringJoiner("&");
            params.forEach((name, value) -> encoded.add(name + "=" + escapeQueryValue(value)));
            path = (sort.equals("date") ? "/search_by_date?" : "/search?") + encoded;
        }

        <H> IndexedPage<H> read(byte[] body, Class<H> type) throws IOException {
            JavaType pageType =
                    PublicResources.PROVIDER_JSON
                            .getTypeFactory()
                            .constructParametricType(IndexedPage.class, type);
            IndexedPage<H> response = PublicResources.PROVIDER_JSON.readValue(body, pageType);
            if (response == null
                    || response.hits == null
                    || response.hits.contains(null)
                    || response.page == null
                    || response.page != page
                    || response.pages == null
                    || response.pages < 0
                    || response.totalHits == null
                    || response.totalHits < 0
                    || response.hits.size() > size)
                throw new IllegalArgumentException("invalid search page");
            return response;
        }

        long next(long pages) {
            return page + 1 < pages ? page + 1 : 0;
        }
    }

    private static String escapeQueryValue(String value) {
        try {
            return java.net.URLEncoder.encode(value, "UTF-8")
                    .replace("*", "%2A")
                    .replace("%7E", "~");
        } catch (java.io.UnsupportedEncodingException impossible) {
            throw new AssertionError(impossible);
        }
    }

    private static final class IndexedPage<H> {
        public List<H> hits;
        public Long page;

        @JsonProperty("nbPages")
        public Long pages;

        @JsonProperty("nbHits")
        public Long totalHits;
    }

    private static class RawHit {
        public String objectID, title, url, author;

        @JsonProperty("story_text")
        public String storyText;

        @JsonProperty("created_at_i")
        public long created;

        @JsonProperty("_tags")
        public List<String> tags;

        long id() {
            if (objectID == null || !objectID.matches("[+-]?[0-9]+"))
                throw new IllegalArgumentException("invalid object ID");
            long id = Long.parseLong(objectID);
            if (id <= 0) throw new IllegalArgumentException("invalid object ID");
            return id;
        }

        Rfc3339NanoTimestamp createdAt() {
            if (created <= 0 || created > 253402300799L)
                throw new IllegalArgumentException("invalid creation time");
            return Rfc3339NanoTimestamp.fromInstant(Instant.ofEpochSecond(created));
        }
    }

    private static final class RawStory extends RawHit {
        public long points;

        @JsonProperty("num_comments")
        public long commentCount;

        StorySearchHit decode() throws IOException {
            long id = id();
            List<String> knownTags = new ArrayList<>();
            if (tags != null)
                for (String recognized : STORY_TAGS)
                    if (tags.contains(recognized)) knownTags.add(recognized);
            SearchText text = SearchText.fromHtml(storyText);
            return new StorySearchHit(
                    id,
                    title,
                    url,
                    author,
                    discussionUrl(id),
                    createdAt(),
                    points,
                    commentCount,
                    knownTags,
                    text.value,
                    text.truncated);
        }
    }

    private static final class RawItem extends RawHit {
        @JsonProperty("story_title")
        public String storyTitle;

        @JsonProperty("comment_text")
        public String comment;

        @JsonProperty("parent_id")
        public long parentId;

        @JsonProperty("story_id")
        public long storyId;

        ItemSearchHit decode(String requestedType) throws IOException {
            long id = id();
            String type = "";
            if (tags != null)
                for (String tag : tags)
                    if (ITEM_TYPES.contains(tag)) {
                        type = tag;
                        break;
                    }
            if (type.isEmpty() || (!requestedType.isEmpty() && !type.equals(requestedType)))
                throw new IllegalArgumentException("unexpected item type");
            String selectedTitle = title == null || title.isEmpty() ? storyTitle : title;
            SearchText text = SearchText.fromHtml(type.equals("comment") ? comment : storyText);
            return new ItemSearchHit(
                    id,
                    selectedTitle,
                    url,
                    author,
                    discussionUrl(id),
                    createdAt(),
                    type,
                    text.value,
                    parentId,
                    storyId);
        }
    }

    private static String discussionUrl(long id) {
        return "https://news.ycombinator.com/item?id=" + id;
    }
}
