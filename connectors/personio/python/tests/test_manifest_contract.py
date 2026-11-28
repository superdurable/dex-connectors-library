# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Personio Python Flow with its generated connection."""

import dex

from dex_connectors.personio.personiomock import Mock
from examples.person_inspection.flow import FLOW_TYPE, PersonInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = PersonInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_single_operation_connections_still_bind_by_position() -> None:
    """The first release supported manually binding getPerson with a positional reference."""
    from dex_connectors.personio import Connection, new_connection_with_operations

    example = Mock().connection()
    query = example.get_person()
    for connection in (
        Connection(example.reference, query),
        new_connection_with_operations(query, example.reference),
    ):
        assert connection.get_person() is query
        dex.Registry([PersonInspectionFlow(connection)], allow_async_handlers=True)
