# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Typeform forms, responses, answers, webhooks, and operation inputs and outputs.

Form titles, answers, and hidden field values are untrusted respondent or author text:
escape them before rendering HTML.
"""

import dataclasses
from dataclasses import dataclass
from datetime import UTC, datetime

ZERO_TIME = datetime(1, 1, 1, tzinfo=UTC)
"""The zero timestamp an omitted Typeform time decodes to."""

_OMIT = {"dex_json_omit_default": True}


def _named(name: str, omit: bool = False) -> dict[str, object]:
    metadata: dict[str, object] = {"dex_json_name": name}
    if omit:
        metadata["dex_json_omit_default"] = True
    return metadata


def _time(name: str) -> dict[str, object]:
    return {**_named(name, omit=True), "dex_json_format": "rfc3339"}


@dataclass(frozen=True, slots=True)
class ListFormsInput:
    """A forms page request; zero page and pageSize use page 1 and Typeform's default of 10."""

    search: str = dataclasses.field(default="", metadata=_OMIT)
    workspace_id: str = dataclasses.field(default="", metadata=_named("workspaceId", omit=True))
    page: int = dataclasses.field(default=0, metadata=_OMIT)
    page_size: int = dataclasses.field(default=0, metadata=_named("pageSize", omit=True))


@dataclass(frozen=True, slots=True)
class FormSummary:
    """One listed form; isPublic is true unless Typeform reports the form as private."""

    id: str = ""
    title: str = ""
    is_public: bool = dataclasses.field(default=False, metadata=_named("isPublic"))
    display_url: str = dataclasses.field(default="", metadata=_named("displayUrl", omit=True))
    created_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("createdAt"))
    last_updated_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("lastUpdatedAt"))


@dataclass(frozen=True, slots=True)
class FormPage:
    """One page of forms; nextPage is zero on the last page."""

    forms: tuple[FormSummary, ...] = ()
    page: int = 0
    page_count: int = dataclasses.field(default=0, metadata=_named("pageCount"))
    total_items: int = dataclasses.field(default=0, metadata=_named("totalItems"))
    next_page: int = dataclasses.field(default=0, metadata=_named("nextPage", omit=True))


@dataclass(frozen=True, slots=True)
class GetFormInput:
    """The form ID, the part after /to/ in the form link."""

    form_id: str = dataclasses.field(default="", metadata=_named("formId"))


@dataclass(frozen=True, slots=True)
class FormFieldChoice:
    """One choice of a multiple-choice, dropdown, or picture-choice field."""

    id: str = dataclasses.field(default="", metadata=_OMIT)
    ref: str = dataclasses.field(default="", metadata=_OMIT)
    label: str = ""


@dataclass(frozen=True, slots=True)
class FormField:
    """One question; fields inside a question group carry the group's ID in groupFieldId."""

    id: str = ""
    ref: str = dataclasses.field(default="", metadata=_OMIT)
    title: str = ""
    type: str = ""
    is_required: bool = dataclasses.field(default=False, metadata=_named("isRequired"))
    group_field_id: str = dataclasses.field(default="", metadata=_named("groupFieldId", omit=True))
    choices: tuple[FormFieldChoice, ...] = dataclasses.field(default=(), metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class Form:
    """One form's fields in display order, flattened from question groups, and hidden field names."""

    id: str = ""
    title: str = ""
    language: str = dataclasses.field(default="", metadata=_OMIT)
    fields: tuple[FormField, ...] = ()
    hidden_fields: tuple[str, ...] = dataclasses.field(default=(), metadata=_named("hiddenFields", omit=True))
    display_url: str = dataclasses.field(default="", metadata=_named("displayUrl", omit=True))


@dataclass(frozen=True, slots=True)
class ListResponsesInput:
    """A completed-responses page request; zero pageSize uses Typeform's default of 25."""

    form_id: str = dataclasses.field(default="", metadata=_named("formId"))
    page_size: int = dataclasses.field(default=0, metadata=_named("pageSize", omit=True))
    since: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("since"))
    until: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("until"))
    before: str = dataclasses.field(default="", metadata=_OMIT)
    after: str = dataclasses.field(default="", metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormAnswerChoice:
    """The selected choice of a single-choice answer; other holds free text."""

    id: str = dataclasses.field(default="", metadata=_OMIT)
    ref: str = dataclasses.field(default="", metadata=_OMIT)
    label: str = dataclasses.field(default="", metadata=_OMIT)
    other: str = dataclasses.field(default="", metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormAnswerChoices:
    """The selected choices of a multiple-choice answer, in matching order."""

    ids: tuple[str, ...] = dataclasses.field(default=(), metadata=_OMIT)
    labels: tuple[str, ...] = dataclasses.field(default=(), metadata=_OMIT)
    refs: tuple[str, ...] = dataclasses.field(default=(), metadata=_OMIT)
    other: str = dataclasses.field(default="", metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormAnswerPayment:
    """A payment answer; amount is Typeform's decimal string."""

    amount: str = ""
    is_successful: bool = dataclasses.field(default=False, metadata=_named("isSuccessful"))


@dataclass(frozen=True, slots=True)
class FormAnswerSignature:
    """A signature answer's image URL and capture method."""

    url: str = ""
    method: str = dataclasses.field(default="", metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormAnswerMultiFormat:
    """A video, audio, or text answer to a multi-format question."""

    text: str = dataclasses.field(default="", metadata=_OMIT)
    video_id: str = dataclasses.field(default="", metadata=_named("videoId", omit=True))
    audio_id: str = dataclasses.field(default="", metadata=_named("audioId", omit=True))
    transcript: str = dataclasses.field(default="", metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormAnswer:
    """One answer; type names the one value field Typeform set, such as text or choices."""

    field_id: str = dataclasses.field(default="", metadata=_named("fieldId"))
    field_ref: str = dataclasses.field(default="", metadata=_named("fieldRef", omit=True))
    field_type: str = dataclasses.field(default="", metadata=_named("fieldType"))
    field_title: str = dataclasses.field(default="", metadata=_named("fieldTitle", omit=True))
    type: str = ""
    text: str = dataclasses.field(default="", metadata=_OMIT)
    email: str = dataclasses.field(default="", metadata=_OMIT)
    url: str = dataclasses.field(default="", metadata=_OMIT)
    file_url: str = dataclasses.field(default="", metadata=_named("fileUrl", omit=True))
    date: str = dataclasses.field(default="", metadata=_OMIT)
    phone_number: str = dataclasses.field(default="", metadata=_named("phoneNumber", omit=True))
    number: float | None = dataclasses.field(default=None, metadata=_OMIT)
    boolean: bool | None = dataclasses.field(default=None, metadata=_OMIT)
    choice: FormAnswerChoice | None = dataclasses.field(default=None, metadata=_OMIT)
    choices: FormAnswerChoices | None = dataclasses.field(default=None, metadata=_OMIT)
    payment: FormAnswerPayment | None = dataclasses.field(default=None, metadata=_OMIT)
    signature: FormAnswerSignature | None = dataclasses.field(default=None, metadata=_OMIT)
    multi_format: FormAnswerMultiFormat | None = dataclasses.field(
        default=None, metadata=_named("multiFormat", omit=True)
    )


@dataclass(frozen=True, slots=True)
class FormVariable:
    """One calculated variable; type is text or number."""

    key: str = ""
    type: str = ""
    text: str = dataclasses.field(default="", metadata=_OMIT)
    number: float | None = dataclasses.field(default=None, metadata=_OMIT)


@dataclass(frozen=True, slots=True)
class FormResponse:
    """One completed response; non-string hidden field values hold their compact JSON text."""

    token: str = ""
    landed_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("landedAt"))
    submitted_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("submittedAt"))
    answers: tuple[FormAnswer, ...] = ()
    hidden_fields: dict[str, str] = dataclasses.field(
        default_factory=dict, metadata=_named("hiddenFields", omit=True)
    )
    variables: tuple[FormVariable, ...] = dataclasses.field(default=(), metadata=_OMIT)
    score: float | None = dataclasses.field(default=None, metadata=_OMIT)
    ending_ref: str = dataclasses.field(default="", metadata=_named("endingRef", omit=True))

    def answer_by_field_ref(self, ref: str) -> FormAnswer | None:
        """Return the answer to the field with this ref, or None."""
        return next((answer for answer in self.answers if ref and answer.field_ref == ref), None)

    def answer_by_field_id(self, field_id: str) -> FormAnswer | None:
        """Return the answer to the field with this ID, or None."""
        return next((answer for answer in self.answers if field_id and answer.field_id == field_id), None)


@dataclass(frozen=True, slots=True)
class ResponsePage:
    """One page of responses, newest first; nextBeforeToken pages to older responses."""

    form_id: str = dataclasses.field(default="", metadata=_named("formId"))
    responses: tuple[FormResponse, ...] = ()
    total_items: int = dataclasses.field(default=0, metadata=_named("totalItems"))
    page_count: int = dataclasses.field(default=0, metadata=_named("pageCount"))
    next_before_token: str = dataclasses.field(default="", metadata=_named("nextBeforeToken", omit=True))


@dataclass(frozen=True, slots=True)
class UpsertWebhookInput:
    """The form, the webhook tag that identifies it, and its HTTPS callback URL."""

    form_id: str = dataclasses.field(default="", metadata=_named("formId"))
    tag: str = ""
    url: str = ""
    is_disabled: bool = dataclasses.field(default=False, metadata=_named("disabled", omit=True))


@dataclass(frozen=True, slots=True)
class Webhook:
    """The stored webhook; eventTypes lists its subscribed events in sorted order."""

    id: str = ""
    form_id: str = dataclasses.field(default="", metadata=_named("formId"))
    tag: str = ""
    url: str = ""
    is_enabled: bool = dataclasses.field(default=False, metadata=_named("isEnabled"))
    event_types: tuple[str, ...] = dataclasses.field(default=(), metadata=_named("eventTypes"))
    created_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("createdAt"))
    updated_at: datetime = dataclasses.field(default=ZERO_TIME, metadata=_time("updatedAt"))
