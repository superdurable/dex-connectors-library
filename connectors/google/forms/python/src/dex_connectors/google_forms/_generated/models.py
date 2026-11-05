# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import RFC3339NanoTimestamp, format_rfc3339_nano

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetFormInput identifies one form.
@dataclass(frozen=True, slots=True)
class GetFormInput:
    # FormID is the form ID, such as the formPicker unit's formId or the ID in
    # https://docs.google.com/forms/d/FORM_ID/edit. It is also the form's Drive file ID.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})


setattr(GetFormInput, '__module__', 'dex_connectors.google_forms.models')


# FormPublishState is a form's publishing state.
@dataclass(frozen=True, slots=True)
class FormPublishState:
    # IsPublished reports that the form is published and visible to responders.
    is_published: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublished'})
    # IsAcceptingResponses reports that the form accepts new responses.
    is_accepting_responses: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAcceptingResponses'})


setattr(FormPublishState, '__module__', 'dex_connectors.google_forms.models')


# FormChoice is one option of a choice question or one column of a grid.
@dataclass(frozen=True, slots=True)
class FormChoice:
    # Value is the option text, which is also the answer value when a respondent picks it.
    value: str = ''
    # IsOther reports the Other option, whose answer value is the respondent's own text.
    is_other: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOther', 'dex_json_omit_default': True})


setattr(FormChoice, '__module__', 'dex_connectors.google_forms.models')


# FormScale is the numeric range of a scale question.
@dataclass(frozen=True, slots=True)
class FormScale:
    # Low is the lowest selectable number.
    low: int = 0
    # High is the highest selectable number.
    high: int = 0
    # LowLabel describes the lowest number.
    low_label: str = _dataclass_field(default='', metadata={'dex_json_name': 'lowLabel', 'dex_json_omit_default': True})
    # HighLabel describes the highest number.
    high_label: str = _dataclass_field(default='', metadata={'dex_json_name': 'highLabel', 'dex_json_omit_default': True})


setattr(FormScale, '__module__', 'dex_connectors.google_forms.models')


# FormQuestion is one question that a FormResponse can answer under QuestionID.
@dataclass(frozen=True, slots=True)
class FormQuestion:
    # QuestionID is the question ID that keys its answer in every FormResponse.
    question_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'questionId'})
    # ItemID is the ID of the item that contains the question.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Title is the question text: the item title for a question item, or the row title for a grid row.
    title: str = ''
    # Type is the question type.
    type: str = _dataclass_field(kw_only=True)
    # IsRequired reports that a respondent must answer the question to submit.
    is_required: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRequired'})
    # Choices lists the options of a choice question, or the columns of a grid row, in form order.
    choices: tuple[FormChoice, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Scale is the range of a scale question.
    scale: FormScale | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # RatingScaleLevel is the number of icons a rating question offers.
    rating_scale_level: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ratingScaleLevel', 'dex_json_omit_default': True})
    # IncludesYear reports that a date question asks for the year.
    includes_year: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesYear', 'dex_json_omit_default': True})
    # IncludesTime reports that a date question asks for a time of day too.
    includes_time: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesTime', 'dex_json_omit_default': True})
    # IsDuration reports that a time question asks for an elapsed time instead of a time of day.
    is_duration: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDuration', 'dex_json_omit_default': True})
    # PointValue is the most points a quiz question awards automatically; zero for an ungraded question.
    point_value: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pointValue', 'dex_json_omit_default': True})


setattr(FormQuestion, '__module__', 'dex_connectors.google_forms.models')


# FormItem is one item of a form.
@dataclass(frozen=True, slots=True)
class FormItem:
    # ItemID is the item ID.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Kind is the item kind.
    kind: str = _dataclass_field(kw_only=True)
    # Title is the item title: the question text of a question item, or the shared prompt of a grid.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Description is the item's help text.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Questions lists the item's questions: one for a question item, one per row for a grid, and
    # none for other kinds.
    questions: tuple[FormQuestion, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(FormItem, '__module__', 'dex_connectors.google_forms.models')


# Form is one form's content in form order.
@dataclass(frozen=True, slots=True)
class Form:
    # FormID is the form ID.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Title is the title respondents see.
    title: str = ''
    # DocumentTitle is the file name shown in Google Drive.
    document_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentTitle', 'dex_json_omit_default': True})
    # Description is the form description respondents see.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ResponderURI is the link respondents open to submit a response; its ID differs from FormID.
    responder_uri: str = _dataclass_field(default='', metadata={'dex_json_name': 'responderUri', 'dex_json_omit_default': True})
    # LinkedSheetID is the Google Sheets spreadsheet that collects responses, when the form has one.
    linked_sheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'linkedSheetId', 'dex_json_omit_default': True})
    # RevisionID changes when the form content changes. Google documents it as opaque and valid for
    # only 24 hours, so compare it only between reads made close together.
    revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'revisionId', 'dex_json_omit_default': True})
    # IsQuiz reports a quiz form, whose responses carry grades and a total score.
    is_quiz: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isQuiz', 'dex_json_omit_default': True})
    # EmailCollectionType is Google's email collection setting, such as DO_NOT_COLLECT, VERIFIED, or
    # RESPONDER_INPUT; blank when Google does not report it. Collected addresses appear as
    # FormResponse.RespondentEmail.
    email_collection_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailCollectionType', 'dex_json_omit_default': True})
    # PublishState reports whether the form is published and accepting responses; nil for a legacy form
    # without publish settings.
    publish_state: FormPublishState | None = _dataclass_field(default=None, metadata={'dex_json_name': 'publishState', 'dex_json_omit_default': True})
    # Items lists every item, including section headers and media, in form order.
    items: tuple[FormItem, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


Form.__annotations__['items'] = tuple[FormItem, ...] | None
Form.__dataclass_fields__['items'].type = tuple[FormItem, ...] | None
setattr(Form, '__module__', 'dex_connectors.google_forms.models')


# ListResponsesInput selects one page of a form's responses. Google documents
# no order for the listing, so read every page before treating a time as complete.
@dataclass(frozen=True, slots=True)
class ListResponsesInput:
    # FormID is the form ID, such as the formPicker unit's formId.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # SubmittedAfter keeps responses submitted after, but not at, this time with Google's
    # "timestamp > N" filter; zero has no lower bound.
    submitted_after: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'submittedAfter', 'dex_json_omit_default': True})
    # SubmittedAtOrAfter keeps responses submitted at or after this time with Google's
    # "timestamp >= N" filter; zero has no lower bound. Set at most one of the two times.
    submitted_at_or_after: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'submittedAtOrAfter', 'dex_json_omit_default': True})
    # PageSize is 1 to 1000 responses; zero uses the connection's responsePageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken continues a listing with its NextPageToken; blank reads the first page. Google requires
    # the same form and filter as the request that returned the token.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.submitted_after is not None:
            timestamp = self.submitted_after
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'submitted_after', canonical)
        if self.submitted_at_or_after is not None:
            timestamp = self.submitted_at_or_after
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'submitted_at_or_after', canonical)


setattr(ListResponsesInput, '__module__', 'dex_connectors.google_forms.models')


# FormAnswerFile is one file uploaded to a file upload question.
@dataclass(frozen=True, slots=True)
class FormAnswerFile:
    # FileID is the uploaded file's Google Drive file ID.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})
    # FileName is the file name stored in Google Drive.
    file_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileName', 'dex_json_omit_default': True})
    # MimeType is the file's MIME type in Google Drive.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})


setattr(FormAnswerFile, '__module__', 'dex_connectors.google_forms.models')


# FormAnswerGrade is the grade of one quiz answer.
@dataclass(frozen=True, slots=True)
class FormAnswerGrade:
    # Score is the points awarded for the answer.
    score: float = 0.0
    # IsCorrect reports a correct answer; a zero score does not imply an incorrect one.
    is_correct: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCorrect'})


setattr(FormAnswerGrade, '__module__', 'dex_connectors.google_forms.models')


# FormAnswer is the answer to one question.
@dataclass(frozen=True, slots=True)
class FormAnswer:
    # QuestionID is the question's ID, as Form.QuestionByID finds it.
    question_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'questionId'})
    # Values holds a text answer as Google formats it: the selected option text, one value per
    # selected option for a checkbox question, the entered text, or a number, date, or time as text.
    # See QuestionType for each format.
    values: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Files lists the Google Drive files uploaded to a file upload question.
    files: tuple[FormAnswerFile, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Grade is the answer's grade when the form is a quiz.
    grade: FormAnswerGrade | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(FormAnswer, '__module__', 'dex_connectors.google_forms.models')


# FormResponse is one submitted response with its answers keyed by question ID.
@dataclass(frozen=True, slots=True)
class FormResponse:
    # ResponseID is the response ID. It stays the same when a respondent edits the response.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId'})
    # FormID is the form the response answers.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # CreatedAt is when the response was first submitted, in UTC.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})
    # LastSubmittedAt is when the response was most recently submitted, in UTC. It is later than
    # CreatedAt after a respondent edits the response, and grade changes do not move it.
    last_submitted_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'lastSubmittedAt'})
    # RespondentEmail is the respondent's address when the form collects it.
    respondent_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'respondentEmail', 'dex_json_omit_default': True})
    # TotalScore is the response's points when the form is a quiz and the response was graded.
    total_score: float | None = _dataclass_field(default=None, metadata={'dex_json_name': 'totalScore', 'dex_json_omit_default': True})
    # Answers lists the answered questions sorted by question ID; Google omits a skipped question.
    answers: tuple[FormAnswer, ...] = ()

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)
        if self.last_submitted_at is not None:
            timestamp = self.last_submitted_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'last_submitted_at', canonical)
        object.__setattr__(self, 'answers', _replace_nil_slices(self.answers, 1))


FormResponse.__annotations__['answers'] = tuple[FormAnswer, ...] | None
FormResponse.__dataclass_fields__['answers'].type = tuple[FormAnswer, ...] | None
setattr(FormResponse, '__module__', 'dex_connectors.google_forms.models')


# ResponsePage is one page of a form's responses in Google's order.
@dataclass(frozen=True, slots=True)
class ResponsePage:
    # FormID is the form the responses answer.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Responses lists the page's responses. Google may return a short or empty page while
    # NextPageToken is set.
    responses: tuple[FormResponse, ...] = ()
    # NextPageToken continues the listing; blank means Google has no further pages.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'responses', _replace_nil_slices(self.responses, 1))


ResponsePage.__annotations__['responses'] = tuple[FormResponse, ...] | None
ResponsePage.__dataclass_fields__['responses'].type = tuple[FormResponse, ...] | None
setattr(ResponsePage, '__module__', 'dex_connectors.google_forms.models')


# GetResponseInput identifies one response of one form.
@dataclass(frozen=True, slots=True)
class GetResponseInput:
    # FormID is the form ID, such as the formPicker unit's formId.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # ResponseID is the response ID, such as a FormResponse ResponseID from listResponses.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId'})


setattr(GetResponseInput, '__module__', 'dex_connectors.google_forms.models')
