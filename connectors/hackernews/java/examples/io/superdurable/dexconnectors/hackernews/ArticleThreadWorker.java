// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hackernews.ArticleThreads.*;
import io.superdurable.dexconnectors.sdk.*;

/** Serve a Flow that reads an indexed public Hacker News article. */
public final class ArticleThreadWorker {
    private ArticleThreadWorker() {}

    /** Serve the Worker, or run one public item by passing --read ARTICLE_ID. */
    public static void main(String[] args) throws Exception {
        ArticleThreadFlow flow = new ArticleThreadFlow(new GetArticleThreadOperation());
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        if (args.length == 2 && "--read".equals(args[0])) {
            ArticleThread article = StoryFeedWorker.readOnce(flow, address, new GetArticleThreadInput(Long.parseLong(args[1])), ArticleThread.class);
            System.out.println(article.discussionUrl + " " + article.comments.size());
            return;
        }
        if (args.length != 0) throw new IllegalArgumentException("expected --read ARTICLE_ID or no arguments");
        StoryFeedWorker.serve(flow, address);
    }

    /** Read the selected article and complete with its nested comments. */
    public static final class ArticleThreadFlow implements Flow<GetArticleThreadInput> {
        private final GetArticleThreadOperation operation;
        private final ConnectionRef connection = new ConnectionRef("hacker-news", "hacker-news-public");

        /** Bind the article Query at Worker registration. */
        public ArticleThreadFlow(GetArticleThreadOperation operation) {
            this.operation = operation;
        }

        @Override public String getFlowType() { return "JavaHackerNewsArticleThread"; }
        @Override public StepList<GetArticleThreadInput> getSteps() {
            return StepList.startStep(new ReadArticle(operation, connection))
                    .otherSteps(new ReturnArticle());
        }
        @Override public PersistenceSchema getPersistenceSchema() { return PersistenceSchema.of(); }
    }

    // dex:group group-id:article group-label:"Article thread"
    // dex:explanation text:"Read the selected article and its indexed public comments."
    public static final class ReadArticle extends QueryStep<GetArticleThreadInput, GetArticleThreadInput, ArticleThread, ArticleThreadResult> {
        ReadArticle(GetArticleThreadOperation operation, ConnectionRef connection) {
            super(ArticleThreads.newGetArticleThreadStep("ReadArticle", GetArticleThreadInput.class,
                    new StepAnnotations("article", "Article thread", "Read the selected article and its indexed public comments."),
                    operation, connection, input -> input).branch("found", ReturnArticle.class).build());
        }
    }

    // dex:group group-id:article group-label:"Article thread"
    // dex:explanation text:"Return the article and its nested comments."
    public static final class ReturnArticle implements Step<ArticleThreadResult> {
        @Override public Class<ArticleThreadResult> getInputType() { return ArticleThreadResult.class; }
        @Override public StepDecision execute(Context context, ArticleThreadResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
