# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Ads page Flow using the listAdAccounts Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.linkedin_ads import (
    AdAccountsPage,
    Connection,
    ListAdAccountsInput,
    new_list_ad_accounts_step,
)

FLOW_TYPE = "LinkedInAdsAccountsListPython"
CONNECTION_NAME = "linkedin-ads"
_CALL_STEP_TYPE = "CallListAdAccounts"
_RESULT = dex.Attribute("linkedin_ads-python-page", AdAccountsPage)


def map_to_operation_input(request: ListAdAccountsInput) -> ListAdAccountsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:linkedin-ads group-label:"Ads"
# dex:explanation text:"Start the Ads request."
class _StartRequest(dex.Step[ListAdAccountsInput]):
    def get_step_type(self) -> str:
        return "StartListAdAccounts"

    def execute(self, context: dex.Context, input: ListAdAccountsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListAdAccounts"), input)


# dex:group group-id:linkedin-ads group-label:"Ads"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AdAccountsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListAdAccounts"

    def execute(self, context: dex.Context, input: QueryResult[AdAccountsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountsFlow(dex.Flow[ListAdAccountsInput]):
    """Run one listAdAccounts read through the static linkedin_ads connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the accounts lookup Flow requires the static linkedin_ads connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_ad_accounts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListAdAccountsInput,
            annotations={
                "groupId": "linkedin-ads",
                "groupLabel": "Ads",
                "explanation": "Read one Ads page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "LinkedInAdsAccountsListPython"

    def get_steps(self) -> dex.StepList[ListAdAccountsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
