// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hackernews.SearchQueries.*;
import io.superdurable.dexconnectors.sdk.*;

/** Serve a Flow that searches the public Hacker News index. */
public final class StorySearchWorker {
    private StorySearchWorker() {}

    /** Serve the Worker, or run one public search by passing --read KEYWORDS. */
    public static void main(String[] args) throws Exception {
        StorySearchFlow flow = new StorySearchFlow(new SearchStoriesOperation());
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        if (args.length == 2 && "--read".equals(args[0])) {
            StorySearchPage page =
                    StoryFeedWorker.readOnce(
                            flow,
                            address,
                            new SearchStoriesInput(args[1], "", null, null, null, 0, 0, 2),
                            StorySearchPage.class);
            System.out.println(page.stories.size() + " hits; next page " + page.nextPage);
            return;
        }
        if (args.length != 0)
            throw new IllegalArgumentException("expected --read KEYWORDS or no arguments");
        StoryFeedWorker.serve(flow, address);
    }

    /** Search public stories by keyword. */
    public static final class StorySearchFlow implements Flow<SearchStoriesInput> {
        private final SearchStoriesOperation operation;
        private final ConnectionRef connection =
                new ConnectionRef("hacker-news", "hacker-news-public");

        /** Bind the search Query at Worker registration. */
        public StorySearchFlow(SearchStoriesOperation operation) {
            this.operation = operation;
        }

        @Override
        public String getFlowType() {
            return "JavaHackerNewsStorySearch";
        }

        @Override
        public StepList<SearchStoriesInput> getSteps() {
            return StepList.startStep(new SearchPage(operation, connection))
                    .otherSteps(new ReturnPage());
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:search group-label:"Keyword search"
    // dex:explanation text:"Search public stories by keyword."
    public static final class SearchPage
            extends QueryStep<
                    SearchStoriesInput, SearchStoriesInput, StorySearchPage, StorySearchResult> {
        SearchPage(SearchStoriesOperation operation, ConnectionRef connection) {
            super(
                    SearchQueries.newSearchStoriesStep(
                                    "SearchPage",
                                    SearchStoriesInput.class,
                                    new StepAnnotations(
                                            "search",
                                            "Keyword search",
                                            "Search public stories by keyword."),
                                    operation,
                                    connection,
                                    input -> input)
                            .branch("searched", ReturnPage.class)
                            .build());
        }
    }

    // dex:group group-id:search group-label:"Keyword search"
    // dex:explanation text:"Return the indexed hits and continuation page."
    public static final class ReturnPage implements Step<StorySearchResult> {
        @Override
        public Class<StorySearchResult> getInputType() {
            return StorySearchResult.class;
        }

        @Override
        public StepDecision execute(Context context, StorySearchResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
