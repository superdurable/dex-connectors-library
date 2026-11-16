# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The postButtonMessage Mutation: a message with Block Kit buttons for the buttonClicked Trigger."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, Final

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    Receipt,
)

from ._generated.definitions import DEFECT, POST_BUTTON_MESSAGE_DEFINITION
from .models import PostButtonMessageInput, PostMessageOutput

if TYPE_CHECKING:
    from .client import Client

BUTTON_STYLE_PRIMARY: Final = "primary"
"""Button style that shows a green button; Slack recommends it for at most one button in a message."""
BUTTON_STYLE_DANGER: Final = "danger"
"""Button style that shows a red button for a destructive or rejecting choice."""

# Slack Block Kit limits: one actions block holds 25 elements, and a section text holds 3000 characters.
_MAXIMUM_BUTTONS: Final = 25
_MAXIMUM_TEXT_CHARACTERS: Final = 3000
_MAXIMUM_ACTION_ID_CHARACTERS: Final = 255
_MAXIMUM_BUTTON_TEXT_CHARACTERS: Final = 75
_MAXIMUM_BUTTON_VALUE_CHARACTERS: Final = 2000


def validate_post_button_message_input(input: PostButtonMessageInput) -> Failure | None:
    """Return a VALIDATION Failure when input breaks a Block Kit limit postButtonMessage enforces, else None.

    The connection's maxMessageCharacters is checked when the operation runs.
    """

    def invalid(message: str) -> Failure:
        return Failure(FailureKind.VALIDATION, "slack", "postButtonMessage", message)

    buttons = input.buttons or ()
    if not input.channel_id.strip():
        return invalid("The channel ID is required.")
    if not input.text.strip() or len(input.text) > _MAXIMUM_TEXT_CHARACTERS:
        return invalid(
            f"The button message text is required and must be at most {_MAXIMUM_TEXT_CHARACTERS} characters."
        )
    if not 1 <= len(buttons) <= _MAXIMUM_BUTTONS:
        return invalid(f"A button message needs 1 through {_MAXIMUM_BUTTONS} buttons.")
    action_ids: set[str] = set()
    for button in buttons:
        if not button.action_id.strip() or len(button.action_id) > _MAXIMUM_ACTION_ID_CHARACTERS:
            return invalid(
                "Each button action ID is required and must be at most "
                f"{_MAXIMUM_ACTION_ID_CHARACTERS} characters."
            )
        if button.action_id in action_ids:
            return invalid("Each button action ID must be unique.")
        if not button.text.strip() or len(button.text) > _MAXIMUM_BUTTON_TEXT_CHARACTERS:
            return invalid(
                "Each button label is required and must be at most "
                f"{_MAXIMUM_BUTTON_TEXT_CHARACTERS} characters."
            )
        if len(button.value) > _MAXIMUM_BUTTON_VALUE_CHARACTERS:
            return invalid(
                f"Each button value must be at most {_MAXIMUM_BUTTON_VALUE_CHARACTERS} characters."
            )
        if button.style and button.style not in (BUTTON_STYLE_PRIMARY, BUTTON_STYLE_DANGER):
            return invalid("Each button style must be primary, danger, or empty.")
        action_ids.add(button.action_id)
    return None


def _button_message_blocks(input: PostButtonMessageInput) -> list[dict[str, Any]]:
    """Repeat the text in a section, because Slack shows only the blocks of a message that has them."""
    elements: list[dict[str, Any]] = []
    for button in input.buttons or ():
        element: dict[str, Any] = {
            "type": "button",
            "action_id": button.action_id,
            "text": {"type": "plain_text", "text": button.text, "emoji": True},
        }
        if button.value:
            element["value"] = button.value
        if button.style:
            element["style"] = button.style
        elements.append(element)
    return [
        {"type": "section", "text": {"type": "mrkdwn", "text": input.text}},
        {"type": "actions", "elements": elements},
    ]


class PostButtonMessageOperation:
    """Post a message with Block Kit buttons, top-level or in a thread, with durable single dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation definition."""
        return POST_BUTTON_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostButtonMessageInput) -> IdempotencyKey:
        """Use the stable Call ID as client message identity; Slack documents no deduplication."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PostButtonMessageInput) -> MutationAttempt[PostMessageOutput]:
        """Validate the buttons, then post the text section and the buttons with chat.postMessage."""
        from .client import _post_message

        failure = validate_post_button_message_input(input)
        if failure is not None:
            return MutationAttempt.branch_result(DEFECT, PostMessageOutput(), Receipt(), failure)
        return await _post_message(
            self.client,
            call,
            "postButtonMessage",
            input.channel_id,
            input.thread_timestamp,
            input.text,
            _button_message_blocks(input),
        )
