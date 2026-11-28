// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import com.fasterxml.jackson.annotation.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;
import java.io.IOException;
import java.time.Clock;
import java.util.*;
import java.util.function.Function;

/** Public Hacker News article and nested comment Query. */
public final class ArticleThreads {
    private ArticleThreads() {}

    /** Identify a public story or poll. */
    public static final class GetArticleThreadInput {
        /** Positive Hacker News item ID. */
        public final long id;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator public GetArticleThreadInput(@JsonProperty("id") long id) {
            this.id = id;
        }
    }

    /** An indexed public comment with its replies. */
    public static final class ArticleComment {
        /** Comment item ID. */
        public final long id;
        /** Public username, when present. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String author;
        /** Untrusted comment HTML. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String textHtml;
        /** Canonical comment permalink. */
        public final String discussionUrl;
        /** Nested replies in provider order. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final List<ArticleComment> replies;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator public ArticleComment(@JsonProperty("id") long id,
                @JsonProperty("author") String author,
                @JsonProperty("textHtml") String textHtml,
                @JsonProperty("discussionUrl") String discussionUrl,
                @JsonProperty("replies") List<ArticleComment> replies) {
            this.id = id;
            this.author = author == null ? "" : author;
            this.textHtml = textHtml == null ? "" : textHtml;
            this.discussionUrl = discussionUrl == null ? "" : discussionUrl;
            this.replies = replies == null ? Collections.emptyList() : Collections.unmodifiableList(new ArrayList<>(replies));
        }
    }

    /** An indexed article and its public comments. */
    public static final class ArticleThread {
        /** Article item ID. */
        public final long id;
        /** Untrusted title. */
        public final String title;
        /** Public submitter, when present. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String author;
        /** External source URL, when present. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String url;
        /** Untrusted submitted HTML. */
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        public final String textHtml;
        /** Canonical discussion permalink. */
        public final String discussionUrl;
        /** Nested comments in provider order. */
        public final List<ArticleComment> comments;

        /** Bind the public fields using their Dex wire names. */
        @JsonCreator public ArticleThread(@JsonProperty("id") long id,
                @JsonProperty("title") String title,
                @JsonProperty("author") String author,
                @JsonProperty("url") String url,
                @JsonProperty("textHtml") String textHtml,
                @JsonProperty("discussionUrl") String discussionUrl,
                @JsonProperty("comments") List<ArticleComment> comments) {
            this.id = id;
            this.title = title == null ? "" : title;
            this.author = author == null ? "" : author;
            this.url = url == null ? "" : url;
            this.textHtml = textHtml == null ? "" : textHtml;
            this.discussionUrl = discussionUrl == null ? "" : discussionUrl;
            this.comments = comments == null ? Collections.emptyList() : Collections.unmodifiableList(new ArrayList<>(comments));
        }
    }

    /** Typed Step result for the article Query. */
    public static final class ArticleThreadResult extends QueryResult<ArticleThread> {
        /** Restore the branch, article, receipt and optional failure from Dex. */
        @JsonCreator public ArticleThreadResult(@JsonProperty("branch") String branch, @JsonProperty("value") ArticleThread value,
                @JsonProperty("receipt") Receipt receipt, @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Build the Step that reads a public article with the application's registered connection. */
    public static <I> QueryStep.ConfigBuilder<I, GetArticleThreadInput, ArticleThread, ArticleThreadResult> newGetArticleThreadStep(
            String stepType, Class<I> inputType, StepAnnotations annotations, Query<GetArticleThreadInput, ArticleThread> operation,
            ConnectionRef connection, Function<I, GetArticleThreadInput> mapInput) {
        if (!"hacker-news".equals(connection.getProvider())) throw new IllegalArgumentException("Hacker News provider is required");
        return QueryStep.newConfig(stepType, inputType, annotations, operation, connection, mapInput, ArticleThreadResult::new);
    }

    /** Read the article and all indexed comments with one bounded Search API request. */
    public static final class GetArticleThreadOperation extends PublicApiQuery<GetArticleThreadInput, ArticleThread> {
        private static final ArticleThread EMPTY = new ArticleThread(0, "", "", "", "", "", Collections.emptyList());

        /** Use the public Algolia endpoint and the manifest timeout and size limit. */
        public GetArticleThreadOperation() { super("getArticleThread", EMPTY, "https://hn.algolia.com/api/v1"); }

        /** Bind a Search API origin, bounded transport and clock for Retry-After. */
        public GetArticleThreadOperation(String baseUrl, ProviderHttpTransport transport, int maxResponseBytes, Clock clock) {
            super("getArticleThread", EMPTY, baseUrl, transport, maxResponseBytes, clock);
        }

        @Override public QueryAttempt<ArticleThread> invoke(Call call, GetArticleThreadInput input) throws InterruptedException {
            if (input.id <= 0) return failed("defect", FailureKind.VALIDATION, "article ID must be positive");
            return read("/items/" + input.id, body -> decodeArticle(body, input.id));
        }

        private QueryAttempt<ArticleThread> decodeArticle(byte[] body, long requestedId) {
            ArticleNode node;
            try { node = PublicResources.PROVIDER_JSON.readValue(body, ArticleNode.class); }
            catch (IOException e) { return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News Search returned malformed article JSON"); }
            if (node == null) return failed("notFound", FailureKind.NOT_FOUND, "Hacker News article was not found");
            if (node.id != requestedId || node.title == null || node.title.isEmpty()) {
                return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News Search returned an invalid article");
            }
            List<ArticleComment> comments = new CommentTreeReader().readChildren(node, 0);
            if (comments == null) return failed("invalidResponse", FailureKind.PROTOCOL, "Hacker News Search returned an invalid comment tree");
            return QueryAttempt.branch("found", new ArticleThread(node.id, node.title, node.author, node.url, node.text, discussionUrl(node.id), comments), Receipt.empty());
        }
    }

    private static final class ArticleNode {
        public long id;
        @JsonProperty("parent_id") public long parentId;
        public String title, author, url, text;
        public List<ArticleNode> children;
    }

    private static final class CommentTreeReader {
        private int remaining = 5000;

        List<ArticleComment> readChildren(ArticleNode parent, int depth) {
            if (depth >= 64) return null;
            if (parent.children == null) return Collections.emptyList();
            if (parent.children.size() > remaining) return null;
            remaining -= parent.children.size();
            List<ArticleComment> comments = new ArrayList<>(parent.children.size());
            for (ArticleNode child : parent.children) {
                if (child == null || child.id <= 0 || (child.parentId != 0 && child.parentId != parent.id)) return null;
                List<ArticleComment> replies = readChildren(child, depth + 1);
                if (replies == null) return null;
                comments.add(new ArticleComment(child.id, child.author, child.text, discussionUrl(child.id), replies));
            }
            return comments;
        }
    }

    private static String discussionUrl(long id) { return "https://news.ycombinator.com/item?id=" + id; }
}
