# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_payment_default_answer as get_payment_default_answer
from .._generated.mock import get_payment_found as get_payment_found
from .._generated.mock import get_payment_not_found as get_payment_not_found
from .._generated.mock import list_payments_default_answer as list_payments_default_answer
from .._generated.mock import list_payments_listed as list_payments_listed
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_PAYMENT_DEFINITION as GET_PAYMENT_DEFINITION
from ..client import LIST_PAYMENTS_DEFINITION as LIST_PAYMENTS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetPaymentInput as GetPaymentInput
from ..models import ListPaymentsInput as ListPaymentsInput
from ..models import Money as Money
from ..models import Payment as Payment
from ..models import PaymentPage as PaymentPage

__all__ = [
    "Mock",
    "list_payments_listed",
    "list_payments_default_answer",
    "get_payment_found",
    "get_payment_not_found",
    "get_payment_default_answer",
]

Mock.__doc__ = "Script Square Queries, defaulting to their manifest answers."

list_payments_listed.__doc__ = "Script the successful listed route."
list_payments_default_answer.__doc__ = "Return the manifest's listPayments defaultAnswer."
get_payment_found.__doc__ = "Script the successful found route."
get_payment_not_found.__doc__ = "Script a missing payment."
get_payment_default_answer.__doc__ = "Return the manifest's getPayment defaultAnswer."
