// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hackernews.SearchQueries.*;
import io.superdurable.dexconnectors.sdk.*;

/** Serve a Flow that searches the public Hacker News index. */
public final class ItemSearchWorker {
    private ItemSearchWorker() {}

    /** Serve the Worker, or run one public search by passing --read KEYWORDS. */
    public static void main(String[] args) throws Exception {
        ItemSearchFlow flow = new ItemSearchFlow(new SearchItemsOperation());
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        if (args.length == 2 && "--read".equals(args[0])) {
            ItemSearchPage page =
                    StoryFeedWorker.readOnce(
                            flow,
                            address,
                            new SearchItemsInput(args[1], "", "", null, 0, 2),
                            ItemSearchPage.class);
            System.out.println(page.items.size() + " hits; next page " + page.nextPage);
            return;
        }
        if (args.length != 0)
            throw new IllegalArgumentException("expected --read KEYWORDS or no arguments");
        StoryFeedWorker.serve(flow, address);
    }

    /** Search public indexed items by keyword. */
    public static final class ItemSearchFlow implements Flow<SearchItemsInput> {
        private final SearchItemsOperation operation;
        private final ConnectionRef connection =
                new ConnectionRef("hacker-news", "hacker-news-public");

        /** Bind the search Query at Worker registration. */
        public ItemSearchFlow(SearchItemsOperation operation) {
            this.operation = operation;
        }

        @Override
        public String getFlowType() {
            return "JavaHackerNewsItemSearch";
        }

        @Override
        public StepList<SearchItemsInput> getSteps() {
            return StepList.startStep(new SearchPage(operation, connection))
                    .otherSteps(new ReturnPage());
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:search group-label:"Keyword search"
    // dex:explanation text:"Search public indexed items by keyword."
    public static final class SearchPage
            extends QueryStep<
                    SearchItemsInput, SearchItemsInput, ItemSearchPage, ItemSearchResult> {
        SearchPage(SearchItemsOperation operation, ConnectionRef connection) {
            super(
                    SearchQueries.newSearchItemsStep(
                                    "SearchPage",
                                    SearchItemsInput.class,
                                    new StepAnnotations(
                                            "search",
                                            "Keyword search",
                                            "Search public indexed items by keyword."),
                                    operation,
                                    connection,
                                    input -> input)
                            .branch("searched", ReturnPage.class)
                            .build());
        }
    }

    // dex:group group-id:search group-label:"Keyword search"
    // dex:explanation text:"Return the indexed hits and continuation page."
    public static final class ReturnPage implements Step<ItemSearchResult> {
        @Override
        public Class<ItemSearchResult> getInputType() {
            return ItemSearchResult.class;
        }

        @Override
        public StepDecision execute(Context context, ItemSearchResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
