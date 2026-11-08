# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SurveyMonkey survey inspection Flow using the listSurveys Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.surveymonkey import (
    Connection,
    ListSurveysInput,
    SurveysPage,
    new_list_surveys_step,
)

FLOW_TYPE = "SurveyMonkeySurveyInspection"
CONNECTION_NAME = "surveymonkey"
_CALL_STEP_TYPE = "CallSurveyMonkey"
_RESULT = dex.Attribute("surveymonkey-surveys-page", SurveysPage)


def map_to_operation_input(request: ListSurveysInput) -> ListSurveysInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:surveymonkey group-label:"SurveyMonkey"
# dex:explanation text:"Start the SurveyMonkey request."
class _StartRequest(dex.Step[ListSurveysInput]):
    def get_step_type(self) -> str:
        return "StartSurveyMonkeyRequest"

    def execute(self, context: dex.Context, input: ListSurveysInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSurveyMonkey"), input)


# dex:group group-id:surveymonkey group-label:"SurveyMonkey"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SurveysPage]]):
    def get_step_type(self) -> str:
        return "CompleteSurveyMonkeyRequest"

    def execute(self, context: dex.Context, input: QueryResult[SurveysPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SurveyInspectionFlow(dex.Flow[ListSurveysInput]):
    """Run one listSurveys read through the static surveymonkey connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the survey inspection requires the static surveymonkey connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_surveys_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListSurveysInput,
            annotations={
                "groupId": "surveymonkey",
                "groupLabel": "SurveyMonkey",
                "explanation": "List surveys accessible to the connected SurveyMonkey account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "SurveyMonkeySurveyInspection"

    def get_steps(self) -> dex.StepList[ListSurveysInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
