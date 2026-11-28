// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.personio.PersonDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read one filtered directory page. */
public final class ListPersonsFlow implements Flow<ListPersonsInput> {
    private final QueryStep<ListPersonsInput, ListPersonsInput, PersonPage, ListPersonsResult>
            query;
    private final ReturnPersonPage complete = new ReturnPersonPage();

    /** Bind the Personio query when the Worker registers this Flow. */
    public ListPersonsFlow(Query<ListPersonsInput, PersonPage> operation) {
        query =
                new QueryStep<ListPersonsInput, ListPersonsInput, PersonPage, ListPersonsResult>(
                        PersonDirectory.newListPersonsStep(
                                        "ListPersons",
                                        ListPersonsInput.class,
                                        new StepAnnotations(
                                                "persons",
                                                "Person directory",
                                                "Read one filtered directory page."),
                                        operation,
                                        new ConnectionRef("personio", "personio"),
                                        input -> input)
                                .branch("listed", ReturnPersonPage.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaPersonioListPersons";
    }

    @Override
    public StepList<ListPersonsInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:persons group-label:"Person directory"
    // dex:explanation text:"Return the selected person fields."
    public static final class ReturnPersonPage implements Step<ListPersonsResult> {
        @Override
        public Class<ListPersonsResult> getInputType() {
            return ListPersonsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListPersonsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
