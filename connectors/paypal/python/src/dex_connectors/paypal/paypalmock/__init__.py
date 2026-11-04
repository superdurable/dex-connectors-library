# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_order_default_answer as get_order_default_answer
from .._generated.mock import get_order_found as get_order_found
from .._generated.mock import get_order_not_found as get_order_not_found
from .._generated.mock import list_transactions_default_answer as list_transactions_default_answer
from .._generated.mock import list_transactions_listed as list_transactions_listed
from .._generated.mock import list_transactions_not_found as list_transactions_not_found
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_ORDER_DEFINITION as GET_ORDER_DEFINITION
from ..client import LIST_TRANSACTIONS_DEFINITION as LIST_TRANSACTIONS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetOrderInput as GetOrderInput
from ..models import ListTransactionsInput as ListTransactionsInput
from ..models import Order as Order
from ..models import TransactionAmount as TransactionAmount
from ..models import TransactionDetail as TransactionDetail
from ..models import TransactionInfo as TransactionInfo
from ..models import TransactionPage as TransactionPage

__all__ = [
    "Mock",
    "get_order_found",
    "get_order_not_found",
    "get_order_default_answer",
    "list_transactions_listed",
    "list_transactions_not_found",
    "list_transactions_default_answer",
]

Mock.__doc__ = "Script PayPal operations, defaulting to their manifest answers."

get_order_found.__doc__ = "Script the found route."
get_order_not_found.__doc__ = "Script a missing order."
get_order_default_answer.__doc__ = "Return the manifest's getOrder default answer."
list_transactions_listed.__doc__ = "Script the listed route."
list_transactions_not_found.__doc__ = "Script a missing transaction resource."
list_transactions_default_answer.__doc__ = "Return the manifest's listTransactions default answer."
