# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import inspect
import json
import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import RetryPolicy, default_connector_step_execute_retry_policy

import dex_connectors.elevenlabs as package
from dex_connectors.elevenlabs import CREATE_SPEECH_DEFINITION, LIST_VOICES_DEFINITION
from dex_connectors.elevenlabs.elevenlabsmock import (
    Mock,
    create_speech_default_answer,
    list_voices_default_answer,
)
from examples.demo.flow import FLOW_TYPE, DemoFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (LIST_VOICES_DEFINITION, CREATE_SPEECH_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == "query"
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
    assert operations["listVoices"]["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
    assert LIST_VOICES_DEFINITION.step_defaults.execute_method_timeout == timedelta(seconds=30)
    assert LIST_VOICES_DEFINITION.step_defaults.execute_durability == 2
    assert LIST_VOICES_DEFINITION.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    speech = operations["createSpeech"]["execution"]
    assert (speech["executeMethodTimeout"], speech["durability"]) == ("150s", "sync")
    assert {key: value for key, value in speech["retry"].items() if key != "reason"} == {
        "initialInterval": "2s",
        "backoffCoefficient": 2,
        "maximumInterval": "30s",
        "maximumAttempts": 3,
        "totalDuration": "5m",
    }
    assert CREATE_SPEECH_DEFINITION.step_defaults.execute_method_timeout == timedelta(seconds=150)
    assert CREATE_SPEECH_DEFINITION.step_defaults.execute_durability == 1
    assert CREATE_SPEECH_DEFINITION.step_defaults.execute_retry == RetryPolicy(
        timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(minutes=5)
    )
    voices = list_voices_default_answer().value
    audio = create_speech_default_answer().value
    assert voices is not None and audio is not None
    assert operations["listVoices"]["mocks"][0]["output"] == {
        "voices": [{"voiceId": voice.voice_id, "name": voice.name} for voice in voices.voices],
        "hasMore": voices.has_more,
    }
    assert operations["createSpeech"]["mocks"][0]["output"] == {
        "audioBase64": audio.audio_base64,
        "contentType": audio.content_type,
    }


def test_factory_parameters_match_declared_branch_fields() -> None:
    document = json.loads((Path(package.__file__).parent / "dex-connector.json").read_text())
    common = {
        "step_type",
        "step_input_type",
        "annotations",
        "connection",
        "connection_name",
        "map_to_operation_input",
        "result_attribute",
        "step_options_override",
    }
    for factory in document["factories"]:
        parameters = set(inspect.signature(getattr(package, factory["name"])).parameters)
        assert parameters - common == {branch["field"] for branch in factory["branches"]}


def test_example_registers_with_mock_connection() -> None:
    flow = DemoFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
