// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hackernews;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hackernews.PublicResources.*;
import io.superdurable.dexconnectors.sdk.*;

/** Serve a Flow that reads an item's public author profile. */
public final class ItemAuthorWorker {
    private ItemAuthorWorker() {}

    /** Serve the Worker, or run one public item by passing --read ITEM_ID. */
    public static void main(String[] args) throws Exception {
        ItemAuthorFlow flow = new ItemAuthorFlow(new GetItemOperation(), new GetUserOperation());
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        if (args.length == 2 && "--read".equals(args[0])) {
            User user = StoryFeedWorker.readOnce(flow, address, new GetItemInput(Long.parseLong(args[1])), User.class);
            System.out.println(user.profileUrl);
            return;
        }
        if (args.length != 0) throw new IllegalArgumentException("expected --read ITEM_ID or no arguments");
        StoryFeedWorker.serve(flow, address);
    }

    /** Resolve an item and route its public author to the profile Query. */
    public static final class ItemAuthorFlow implements Flow<GetItemInput> {
        private final GetItemOperation getItem;
        private final GetUserOperation getUser;
        private final ConnectionRef connection = new ConnectionRef("hacker-news", "hacker-news-public");

        /** Bind both operations at Worker registration. */
        public ItemAuthorFlow(GetItemOperation getItem, GetUserOperation getUser) {
            this.getItem = getItem;
            this.getUser = getUser;
        }

        @Override public String getFlowType() { return "JavaHackerNewsItemAuthor"; }
        @Override public StepList<GetItemInput> getSteps() {
            return StepList.startStep(new ReadItem(getItem, connection))
                    .otherSteps(new ReadAuthor(getUser, connection), new ReturnProfile());
        }
        @Override public PersistenceSchema getPersistenceSchema() { return PersistenceSchema.of(); }
    }

    // dex:group group-id:item-author group-label:"Item author"
    // dex:explanation text:"Read the selected public item."
    public static final class ReadItem extends QueryStep<GetItemInput, GetItemInput, Item, ItemResult> {
        ReadItem(GetItemOperation operation, ConnectionRef connection) {
            super(PublicResources.newGetItemStep("ReadItem", GetItemInput.class,
                    new StepAnnotations("item-author", "Item author", "Read the selected public item."),
                    operation, connection, input -> input).branch("found", ReadAuthor.class).build());
        }
    }

    // dex:group group-id:item-author group-label:"Item author"
    // dex:explanation text:"Read the public profile of the selected item's author."
    public static final class ReadAuthor extends QueryStep<ItemResult, GetUserInput, User, UserResult> {
        ReadAuthor(GetUserOperation operation, ConnectionRef connection) {
            super(PublicResources.newGetUserStep("ReadAuthor", ItemResult.class,
                    new StepAnnotations("item-author", "Item author", "Read the public profile of the selected item's author."),
                    operation, connection, result -> new GetUserInput(result.getValue().by))
                    .branch("found", ReturnProfile.class).build());
        }
    }

    // dex:group group-id:item-author group-label:"Item author"
    // dex:explanation text:"Return the public author profile."
    public static final class ReturnProfile implements Step<UserResult> {
        @Override public Class<UserResult> getInputType() { return UserResult.class; }
        @Override public StepDecision execute(Context context, UserResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
