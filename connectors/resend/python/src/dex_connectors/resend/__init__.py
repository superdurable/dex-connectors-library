# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Resend email operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_EMAIL_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEND_EMAIL_DEFINITION,
    SENT,
    Client,
    Config,
    Credentials,
    GetEmailOperation,
    SendEmailOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Email, GetEmailInput, SendEmailInput, SentEmail
from .steps import new_get_email_step, new_send_email_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "Email",
    "GetEmailInput",
    "GetEmailOperation",
    "SendEmailInput",
    "SendEmailOperation",
    "SentEmail",
    "new_connection",
    "new_connection_with_operations",
    "new_get_email_step",
    "new_project_connection",
    "new_send_email_step",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_EMAIL_DEFINITION",
    "INVALID_RESPONSE",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
    "SEND_EMAIL_DEFINITION",
    "SENT",
]
