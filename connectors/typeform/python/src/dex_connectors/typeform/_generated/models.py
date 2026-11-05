# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListFormsInput selects one page of the account's forms.
@dataclass(frozen=True, slots=True)
class ListFormsInput:
    # Search returns only forms whose title contains this text; blank lists every form.
    search: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # WorkspaceID lists only the forms of one workspace, such as the ID in
    # https://admin.typeform.com/accounts/<account>/workspaces/<workspaceId>; blank lists every workspace.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId', 'dex_json_omit_default': True})
    # Page is the 1-based page number; zero reads the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to 200 forms per page; zero uses Typeform's default of 10.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListFormsInput, '__module__', 'dex_connectors.typeform.models')


# FormSummary describes one form in a listing.
@dataclass(frozen=True, slots=True)
class FormSummary:
    # ID is the form ID, the part after /to/ in the form link.
    id: str = ''
    # Title is the form's title.
    title: str = ''
    # IsPublic is false for a form that does not accept responses from its public link.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic'})
    # DisplayURL is the public link respondents open.
    display_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayUrl', 'dex_json_omit_default': True})
    # CreatedAt is when the form was created, in UTC.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LastUpdatedAt is when the form last changed, in UTC.
    last_updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastUpdatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(FormSummary, '__module__', 'dex_connectors.typeform.models')


# FormPage is one page of the account's forms.
@dataclass(frozen=True, slots=True)
class FormPage:
    # Forms are the forms on this page, in Typeform's order.
    forms: tuple[FormSummary, ...] = ()
    # Page is the page number that was read.
    page: int = 0
    # PageCount is how many pages the listing has.
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})
    # TotalItems is how many forms match the request.
    total_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalItems'})
    # NextPage is the page number to read next, or zero on the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'forms', _replace_nil_slices(self.forms, 1))


FormPage.__annotations__['forms'] = tuple[FormSummary, ...] | None
FormPage.__dataclass_fields__['forms'].type = tuple[FormSummary, ...] | None
setattr(FormPage, '__module__', 'dex_connectors.typeform.models')


# GetFormInput names the form to read.
@dataclass(frozen=True, slots=True)
class GetFormInput:
    # FormID is the form ID, such as the formPicker unit's formId or u6nXL7 from
    # https://form.typeform.com/to/u6nXL7.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})


setattr(GetFormInput, '__module__', 'dex_connectors.typeform.models')


# FormFieldChoice is one answer choice of a question.
@dataclass(frozen=True, slots=True)
class FormFieldChoice:
    # ID is the choice ID.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Ref is the choice ref.
    ref: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Label is the choice text.
    label: str = ''


setattr(FormFieldChoice, '__module__', 'dex_connectors.typeform.models')


# FormField is one question of a form.
@dataclass(frozen=True, slots=True)
class FormField:
    # ID is the field ID that every FormAnswer's FieldID names.
    id: str = ''
    # Ref is the field's ref. A ref set through the Create API is stable; Typeform documents the ref it
    # generates for a form built in its editor as non-persistent, so match such answers by ID.
    ref: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Title is the question text, which can hold recall placeholders in double braces.
    title: str = ''
    # Type is the question type, such as short_text, email, multiple_choice, or group.
    type: str = ''
    # IsRequired is true when the respondent must answer the question.
    is_required: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRequired'})
    # GroupFieldID is the ID of the group or contact_info field that contains this question; blank at the
    # top level.
    group_field_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupFieldId', 'dex_json_omit_default': True})
    # Choices lists the answer choices of a choice question.
    choices: tuple[FormFieldChoice, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(FormField, '__module__', 'dex_connectors.typeform.models')


# Form is one form's question structure.
@dataclass(frozen=True, slots=True)
class Form:
    # ID is the form ID.
    id: str = ''
    # Title is the form's title.
    title: str = ''
    # Language is the form's language code, such as en.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Fields lists every question in form order. A group's questions follow the group, with
    # GroupFieldID set to the group's ID.
    fields: tuple[FormField, ...] = ()
    # HiddenFields are the names of the form's hidden fields, which each FormResponse's HiddenFields keys.
    hidden_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'hiddenFields', 'dex_json_omit_default': True})
    # DisplayURL is the public link respondents open.
    display_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


Form.__annotations__['fields'] = tuple[FormField, ...] | None
Form.__dataclass_fields__['fields'].type = tuple[FormField, ...] | None
setattr(Form, '__module__', 'dex_connectors.typeform.models')


# ListResponsesInput selects one page of a form's completed responses, newest submission first.
@dataclass(frozen=True, slots=True)
class ListResponsesInput:
    # FormID is the form ID, such as the formPicker unit's formId.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # PageSize is 1 to 1000 responses; zero uses Typeform's default of 25.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Since keeps responses submitted at or after this time, to the second; zero has no lower bound.
    since: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Until keeps responses submitted at or before this time, to the second; zero has no upper bound.
    until: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Before keeps responses submitted before the response with this token, such as an earlier page's
    # NextBeforeToken; blank starts at the newest response.
    before: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # After keeps responses submitted after the response with this token, such as the newest token a Flow
    # already processed; blank has no lower cursor. Set Before or After, not both.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListResponsesInput, '__module__', 'dex_connectors.typeform.models')


# FormAnswerChoice is one selected choice, or the respondent's own text when Other is set.
@dataclass(frozen=True, slots=True)
class FormAnswerChoice:
    # ID is the choice ID.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Ref is the choice ref.
    ref: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Label is the choice text.
    label: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Other is the respondent's own answer for an Other option.
    other: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(FormAnswerChoice, '__module__', 'dex_connectors.typeform.models')


# FormAnswerChoices are the selected choices; IDs, Labels, and Refs are parallel lists when present.
@dataclass(frozen=True, slots=True)
class FormAnswerChoices:
    # IDs are the selected choice IDs.
    ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Labels are the selected choice texts.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Refs are the selected choice refs.
    refs: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Other is the respondent's own answer for an Other option.
    other: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(FormAnswerChoices, '__module__', 'dex_connectors.typeform.models')


# FormAnswerPayment is the connector-safe part of a payment answer.
@dataclass(frozen=True, slots=True)
class FormAnswerPayment:
    # Amount is the charged amount in the form's currency, as Typeform's decimal string.
    amount: str = ''
    # IsSuccessful is true when Typeform processed the payment.
    is_successful: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSuccessful'})


setattr(FormAnswerPayment, '__module__', 'dex_connectors.typeform.models')


# FormAnswerSignature is a signature answer.
@dataclass(frozen=True, slots=True)
class FormAnswerSignature:
    # URL is the signature image's download link.
    url: str = ''
    # Method is how the respondent signed: typed, drawn, or uploaded.
    method: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(FormAnswerSignature, '__module__', 'dex_connectors.typeform.models')


# FormAnswerMultiFormat is a video or audio question's answer.
@dataclass(frozen=True, slots=True)
class FormAnswerMultiFormat:
    # Text is the respondent's typed answer when they chose not to record.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # VideoID identifies a recorded video for the Responses API's media download.
    video_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'videoId', 'dex_json_omit_default': True})
    # AudioID identifies a recorded audio answer for the Responses API's media download.
    audio_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'audioId', 'dex_json_omit_default': True})
    # Transcript is the recording's transcript; Typeform may deliver it later than the webhook.
    transcript: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(FormAnswerMultiFormat, '__module__', 'dex_connectors.typeform.models')


# FormAnswer is one typed answer, keyed by its field. Exactly one value field is set for a known Type;
# an answer type this connector does not know keeps its field and Type with no value.
@dataclass(frozen=True, slots=True)
class FormAnswer:
    # FieldID is the question's field ID, as Form.Fields lists it.
    field_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fieldId'})
    # FieldRef is the question's ref; a webhook fills it from the form definition when the answer omits it.
    field_ref: str = _dataclass_field(default='', metadata={'dex_json_name': 'fieldRef', 'dex_json_omit_default': True})
    # FieldType is the question type, such as short_text, multiple_choice, or rating.
    field_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'fieldType'})
    # FieldTitle is the question text from the webhook's form definition; listResponses leaves it blank.
    field_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'fieldTitle', 'dex_json_omit_default': True})
    # Type is the answer type, one of the AnswerType constants, which names the value field that is set.
    type: str = ''
    # Text is a text answer.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is an email answer.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is a url answer, such as a website or a Calendly booking link.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FileURL is the download link of an uploaded file; Typeform does not promise its shape.
    file_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileUrl', 'dex_json_omit_default': True})
    # Date is a date answer exactly as Typeform sends it: YYYY-MM-DD in webhooks, and an RFC 3339
    # midnight timestamp in listResponses.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PhoneNumber is a phone number answer, such as +14155550100.
    phone_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'phoneNumber', 'dex_json_omit_default': True})
    # Number is a number, rating, or opinion scale answer.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Boolean is a yes/no or legal answer.
    boolean: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Choice is a single selected choice.
    choice: FormAnswerChoice | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Choices are the selected choices, in the respondent's order for a ranking question.
    choices: FormAnswerChoices | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Payment is a payment answer. The card's last four digits and cardholder name are not copied.
    payment: FormAnswerPayment | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Signature is a signature answer.
    signature: FormAnswerSignature | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # MultiFormat is a video or audio question's answer, or its typed text alternative.
    multi_format: FormAnswerMultiFormat | None = _dataclass_field(default=None, metadata={'dex_json_name': 'multiFormat', 'dex_json_omit_default': True})


setattr(FormAnswer, '__module__', 'dex_connectors.typeform.models')


# FormVariable is one form variable and its final value.
@dataclass(frozen=True, slots=True)
class FormVariable:
    # Key is the variable name, such as score.
    key: str = ''
    # Type is text or number and names the value field that is set.
    type: str = ''
    # Text is a text variable's value.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Number is a number variable's value.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(FormVariable, '__module__', 'dex_connectors.typeform.models')


# FormResponse is one completed response with typed answers. The responseSubmitted Trigger and
# listResponses return the same shape, so a Flow can process pushed and backfilled responses alike.
@dataclass(frozen=True, slots=True)
class FormResponse:
    # Token identifies the response. Typeform documents it as the response ID, and listResponses pages
    # with it through before and after.
    token: str = ''
    # LandedAt is when the respondent opened the form, in UTC.
    landed_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'landedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SubmittedAt is when the respondent submitted the form, in UTC.
    submitted_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'submittedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Answers lists the answered questions; Typeform omits a question the respondent skipped.
    answers: tuple[FormAnswer, ...] = ()
    # HiddenFields maps each hidden field name to its value; a non-string value keeps its JSON text.
    hidden_fields: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'hiddenFields', 'dex_json_omit_default': True})
    # Variables lists the form's variables and their final values, such as score.
    variables: tuple[FormVariable, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Score is the calculated score when the form computes one.
    score: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # EndingRef is the ref of the ending screen the respondent reached, from webhooks only.
    ending_ref: str = _dataclass_field(default='', metadata={'dex_json_name': 'endingRef', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'answers', _replace_nil_slices(self.answers, 1))


FormResponse.__annotations__['answers'] = tuple[FormAnswer, ...] | None
FormResponse.__dataclass_fields__['answers'].type = tuple[FormAnswer, ...] | None
setattr(FormResponse, '__module__', 'dex_connectors.typeform.models')


# ResponsePage is one page of a form's completed responses.
@dataclass(frozen=True, slots=True)
class ResponsePage:
    # FormID is the form the responses answer.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Responses are newest submission first.
    responses: tuple[FormResponse, ...] = ()
    # TotalItems is how many responses match the filters.
    total_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalItems'})
    # PageCount is how many pages of this size match the filters.
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})
    # NextBeforeToken is the oldest response's token when older responses remain; pass it as Before to
    # read the next page. It is blank on the last page and for a request that sets After.
    next_before_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextBeforeToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'responses', _replace_nil_slices(self.responses, 1))


ResponsePage.__annotations__['responses'] = tuple[FormResponse, ...] | None
ResponsePage.__dataclass_fields__['responses'].type = tuple[FormResponse, ...] | None
setattr(ResponsePage, '__module__', 'dex_connectors.typeform.models')


# UpsertWebhookInput creates or replaces the webhook a form has under Tag.
@dataclass(frozen=True, slots=True)
class UpsertWebhookInput:
    # FormID is the form ID, such as the formPicker unit's formId.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Tag names the webhook within the form, such as dex-response-recorder; the same tag always updates
    # the same webhook.
    tag: str = ''
    # URL is the public HTTPS URL where the application mounts the responseSubmitted endpoint, such as
    # https://example.com/webhooks/typeform. Typeform rejects new http URLs.
    url: str = ''
    # IsDisabled stores the webhook without delivering to it; false enables delivery.
    disabled: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(UpsertWebhookInput, '__module__', 'dex_connectors.typeform.models')


# Webhook is one form webhook as Typeform stored it. Its signing secret is never copied.
@dataclass(frozen=True, slots=True)
class Webhook:
    # ID is Typeform's webhook ID.
    id: str = ''
    # FormID is the form the webhook belongs to.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Tag is the webhook's tag.
    tag: str = ''
    # URL is where Typeform delivers.
    url: str = ''
    # IsEnabled is true when Typeform delivers submissions to URL.
    is_enabled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEnabled'})
    # EventTypes are the subscribed event types, such as form_response, in sorted order.
    event_types: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'eventTypes'})
    # CreatedAt is when the webhook was first created, in UTC.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the webhook last changed, in UTC.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'event_types', _replace_nil_slices(self.event_types, 1))


Webhook.__annotations__['event_types'] = tuple[str, ...] | None
Webhook.__dataclass_fields__['event_types'].type = tuple[str, ...] | None
setattr(Webhook, '__module__', 'dex_connectors.typeform.models')
