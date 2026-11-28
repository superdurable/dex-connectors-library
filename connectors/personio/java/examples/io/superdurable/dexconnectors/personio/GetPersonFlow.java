// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.personio.PersonDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read the selected person. */
public final class GetPersonFlow implements Flow<GetPersonInput> {
    private final QueryStep<GetPersonInput, GetPersonInput, PersonProfile, GetPersonResult> query;
    private final ReturnPersonProfile complete = new ReturnPersonProfile();

    /** Bind the Personio query when the Worker registers this Flow. */
    public GetPersonFlow(Query<GetPersonInput, PersonProfile> operation) {
        query =
                new QueryStep<GetPersonInput, GetPersonInput, PersonProfile, GetPersonResult>(
                        PersonDirectory.newGetPersonStep(
                                        "GetPerson",
                                        GetPersonInput.class,
                                        new StepAnnotations(
                                                "persons",
                                                "Person directory",
                                                "Read the selected person."),
                                        operation,
                                        new ConnectionRef("personio", "personio"),
                                        input -> input)
                                .branch("found", ReturnPersonProfile.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaPersonioGetPerson";
    }

    @Override
    public StepList<GetPersonInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:persons group-label:"Person directory"
    // dex:explanation text:"Return the selected person fields."
    public static final class ReturnPersonProfile implements Step<GetPersonResult> {
        @Override
        public Class<GetPersonResult> getInputType() {
            return GetPersonResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetPersonResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
