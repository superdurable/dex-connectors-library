// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.*;

import io.superdurable.dexconnectors.sdk.*;

import java.util.function.Function;

/** Operation-specific Dex Step factories for Personio. */
public final class PersonDirectory {
    private PersonDirectory() {}

    /** Concrete Dex Step input for a GetPerson result. */
    public static final class GetPersonResult extends QueryResult<PersonProfile> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetPersonResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") PersonProfile value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the GetPerson query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, GetPersonInput, PersonProfile, GetPersonResult>
                    newGetPersonStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetPersonInput, PersonProfile> operation,
                            ConnectionRef connection,
                            Function<I, GetPersonInput> mapInput) {
        if (!"personio".equals(connection.getProvider())) {
            throw new IllegalArgumentException("Personio connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetPersonResult::new);
    }

    /** Concrete Dex Step input for a ListPersons result. */
    public static final class ListPersonsResult extends QueryResult<PersonPage> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListPersonsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") PersonPage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the ListPersons query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListPersonsInput, PersonPage, ListPersonsResult>
                    newListPersonsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListPersonsInput, PersonPage> operation,
                            ConnectionRef connection,
                            Function<I, ListPersonsInput> mapInput) {
        if (!"personio".equals(connection.getProvider())) {
            throw new IllegalArgumentException("Personio connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListPersonsResult::new);
    }
}
