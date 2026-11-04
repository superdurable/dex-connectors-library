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
from .._generated.mock import list_orders_default_answer as list_orders_default_answer
from .._generated.mock import list_orders_listed as list_orders_listed
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_ORDER_DEFINITION as GET_ORDER_DEFINITION
from ..client import LIST_ORDERS_DEFINITION as LIST_ORDERS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetOrderInput as GetOrderInput
from ..models import ListOrdersInput as ListOrdersInput
from ..models import Order as Order
from ..models import OrderPage as OrderPage

__all__ = [
    "Mock",
    "list_orders_listed",
    "list_orders_default_answer",
    "get_order_found",
    "get_order_not_found",
    "get_order_default_answer",
]

Mock.__doc__ = "Script Shopify Queries, defaulting to their manifest answers."

list_orders_listed.__doc__ = "Script the successful listed route."
list_orders_default_answer.__doc__ = "Return the manifest's listPayments defaultAnswer."
get_order_found.__doc__ = "Script the successful found route."
get_order_not_found.__doc__ = "Script a missing order."
get_order_default_answer.__doc__ = "Return the manifest's getPayment defaultAnswer."
