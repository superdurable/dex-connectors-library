# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Strict conversion of Google Forms resources into generated result models."""

import re
from datetime import UTC, datetime, timedelta
from typing import Any

from .client import is_google_id
from .models import (
    Form,
    FormAnswer,
    FormAnswerFile,
    FormAnswerGrade,
    FormChoice,
    FormItem,
    FormPublishState,
    FormQuestion,
    FormResponse,
    FormScale,
    ResponsePage,
)

_TIME = re.compile(r"(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:[.,](\d+))?(Z|[+-]\d\d:\d\d)\Z")


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("provider object has an invalid type")
    return value


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("provider string has an invalid type")
    return value


def _bool(value: Any) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("provider boolean has an invalid type")
    return value


def _number(value: Any) -> float:
    if type(value) not in (int, float):
        raise ValueError("provider number has an invalid type")
    return float(value)


def _integer(value: Any) -> int:
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError("provider integer has an invalid type")
    return value


def _array(value: Any) -> list[Any]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("provider array has an invalid type")
    return value


def _time(value: Any) -> str:
    raw = _string(value)
    if not raw:
        return "0001-01-01T00:00:00Z"
    match = _TIME.fullmatch(raw)
    if not match:
        raise ValueError("provider time has an invalid format")
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction = (match.group(7) or "")[:9]
    offset = match.group(8)
    if hour > 23 or minute > 59 or second > 59 or (offset != "Z" and int(offset[1:3]) > 24):
        raise ValueError("provider time is invalid")
    try:
        parsed = datetime(year, month, day, hour, minute, second, int((fraction + "000000")[:6]), UTC)
        if offset != "Z":
            hours, minutes = int(offset[1:3]), int(offset[4:6])
            if minutes > 59:
                raise ValueError("provider time offset is invalid")
            delta = timedelta(hours=hours, minutes=minutes)
            parsed += delta if offset[0] == "-" else -delta
        fractional = f".{fraction.rstrip('0')}" if fraction.rstrip("0") else ""
        return parsed.isoformat(timespec="seconds").replace("+00:00", fractional + "Z")
    except (ValueError, OverflowError) as error:
        raise ValueError("provider time is invalid") from error


def _choice(value: Any) -> FormChoice:
    option = _object(value)
    return FormChoice(_string(option.get("value")), _bool(option.get("isOther")))


def _question(value: Any, item_id: str, title: str, grid_columns: dict[str, Any] | None) -> FormQuestion:
    resource = _object(value)
    question_id = _string(resource.get("questionId"))
    if not question_id:
        raise ValueError("form question lacks an ID")
    variants = (
        "choiceQuestion",
        "textQuestion",
        "scaleQuestion",
        "dateQuestion",
        "timeQuestion",
        "fileUploadQuestion",
        "rowQuestion",
        "ratingQuestion",
    )
    present = [name for name in variants if resource.get(name) is not None]
    if len(present) > 1:
        raise ValueError("form question has more than one kind")
    question_type = "unknown"
    choices: tuple[FormChoice, ...] = ()
    scale = None
    rating = 0
    includes_year = includes_time = is_duration = False
    if present:
        variant = present[0]
        content = _object(resource[variant])
        if variant == "choiceQuestion":
            question_type = {"RADIO": "radio", "CHECKBOX": "checkbox", "DROP_DOWN": "dropDown"}.get(
                _string(content.get("type")), "unknown"
            )
            choices = tuple(_choice(option) for option in _array(content.get("options")))
        elif variant == "textQuestion":
            question_type = "paragraph" if _bool(content.get("paragraph")) else "shortText"
        elif variant == "scaleQuestion":
            question_type = "scale"
            scale = FormScale(
                _integer(content.get("low")),
                _integer(content.get("high")),
                _string(content.get("lowLabel")),
                _string(content.get("highLabel")),
            )
        elif variant == "dateQuestion":
            question_type = "date"
            includes_year = _bool(content.get("includeYear"))
            includes_time = _bool(content.get("includeTime"))
        elif variant == "timeQuestion":
            question_type = "time"
            is_duration = _bool(content.get("duration"))
        elif variant == "fileUploadQuestion":
            question_type = "fileUpload"
        elif variant == "ratingQuestion":
            question_type = "rating"
            rating = _integer(content.get("ratingScaleLevel"))
        elif variant == "rowQuestion" and grid_columns is not None:
            question_type = {"RADIO": "radioGrid", "CHECKBOX": "checkboxGrid"}.get(
                _string(grid_columns.get("type")), "unknown"
            )
            choices = tuple(_choice(option) for option in _array(grid_columns.get("options")))
    grading = resource.get("grading")
    point_value = _integer(_object(grading).get("pointValue")) if grading is not None else 0
    return FormQuestion(
        question_id=question_id,
        item_id=item_id,
        title=title,
        type=question_type,
        is_required=_bool(resource.get("required")),
        choices=choices,
        scale=scale,
        rating_scale_level=rating,
        includes_year=includes_year,
        includes_time=includes_time,
        is_duration=is_duration,
        point_value=point_value,
    )


def _item(value: Any) -> FormItem:
    resource = _object(value)
    item_id = _string(resource.get("itemId"))
    if not item_id:
        raise ValueError("form item lacks an ID")
    title = _string(resource.get("title"))
    variants = ("questionItem", "questionGroupItem", "pageBreakItem", "textItem", "imageItem", "videoItem")
    present = [name for name in variants if resource.get(name) is not None]
    if len(present) > 1:
        raise ValueError("form item has more than one kind")
    kind = "unknown"
    questions: tuple[FormQuestion, ...] = ()
    if present:
        variant = present[0]
        kind = {
            "questionItem": "question",
            "questionGroupItem": "questionGroup",
            "pageBreakItem": "pageBreak",
            "textItem": "text",
            "imageItem": "image",
            "videoItem": "video",
        }[variant]
        content = _object(resource[variant])
        if variant == "questionItem":
            questions = (_question(content.get("question"), item_id, title, None),)
        elif variant == "questionGroupItem":
            grid = content.get("grid")
            columns = _object(_object(grid).get("columns")) if grid is not None else None
            questions = tuple(
                _question(row, item_id, _string(_object(row).get("rowQuestion", {}).get("title")), columns)
                for row in _array(content.get("questions"))
            )
    return FormItem(
        item_id=item_id,
        kind=kind,
        title=title,
        description=_string(resource.get("description")),
        questions=questions,
    )


def decode_form(value: Any, form_id: str) -> Form:
    """Validate and map one forms.get resource."""
    resource = _object(value)
    resource_id = _string(resource.get("formId"))
    if not is_google_id(resource_id) or resource_id != form_id:
        raise ValueError("form ID is invalid")
    info = _object(resource.get("info") or {})
    settings = _object(resource.get("settings") or {})
    quiz = _object(settings.get("quizSettings") or {})
    publish = resource.get("publishSettings")
    publish_state = None
    if publish is not None:
        state = _object(publish).get("publishState")
        if state is not None:
            state = _object(state)
            publish_state = FormPublishState(
                _bool(state.get("isPublished")), _bool(state.get("isAcceptingResponses"))
            )
    items = tuple(_item(item) for item in _array(resource.get("items")))
    question_ids = [question.question_id for item in items for question in item.questions]
    if len(question_ids) != len(set(question_ids)):
        raise ValueError("form repeats a question ID")
    return Form(
        form_id=resource_id,
        title=_string(info.get("title")),
        document_title=_string(info.get("documentTitle")),
        description=_string(info.get("description")),
        responder_uri=_string(resource.get("responderUri")),
        linked_sheet_id=_string(resource.get("linkedSheetId")),
        revision_id=_string(resource.get("revisionId")),
        is_quiz=_bool(quiz.get("isQuiz")),
        email_collection_type=_string(settings.get("emailCollectionType")),
        publish_state=publish_state,
        items=items,
    )


def decode_response(value: Any, form_id: str) -> FormResponse:
    """Validate and map one Google response, sorting answers by question ID."""
    resource = _object(value)
    response_id = _string(resource.get("responseId"))
    submitted = _time(resource.get("lastSubmittedTime"))
    if not is_google_id(response_id) or submitted == "0001-01-01T00:00:00Z":
        raise ValueError("response lacks a valid ID or submission time")
    if _string(resource.get("formId")) not in ("", form_id):
        raise ValueError("response belongs to a different form")
    raw_answers = resource.get("answers")
    if raw_answers is None:
        raw_answers = {}
    if not isinstance(raw_answers, dict):
        raise ValueError("response answers have an invalid type")
    answers: list[FormAnswer] = []
    for question_id in sorted(raw_answers):
        raw_answer = _object(raw_answers[question_id])
        if not question_id or _string(raw_answer.get("questionId")) not in ("", question_id):
            raise ValueError("answer question ID is invalid")
        text = raw_answer.get("textAnswers")
        files = raw_answer.get("fileUploadAnswers")
        grade = raw_answer.get("grade")
        values = (
            tuple(_string(_object(item).get("value")) for item in _array(_object(text).get("answers")))
            if text is not None
            else ()
        )
        file_values = (
            tuple(
                FormAnswerFile(
                    _string(_object(item).get("fileId")),
                    _string(_object(item).get("fileName")),
                    _string(_object(item).get("mimeType")),
                )
                for item in _array(_object(files).get("answers"))
            )
            if files is not None
            else ()
        )
        grade_value = (
            FormAnswerGrade(
                _number(0 if _object(grade).get("score") is None else _object(grade)["score"]),
                _bool(_object(grade).get("correct")),
            )
            if grade is not None
            else None
        )
        answers.append(FormAnswer(question_id, values, file_values, grade_value))
    total_score = resource.get("totalScore")
    return FormResponse(
        response_id=response_id,
        form_id=form_id,
        created_at=_time(resource.get("createTime")),
        last_submitted_at=submitted,
        respondent_email=_string(resource.get("respondentEmail")),
        total_score=_number(total_score) if total_score is not None else None,
        answers=tuple(answers),
    )


def decode_page(value: Any, form_id: str, page_size: int) -> ResponsePage:
    """Validate a bounded response page and reject duplicate response IDs."""
    resource = _object(value)
    responses = _array(resource.get("responses"))
    if len(responses) > page_size:
        raise ValueError("page exceeds requested size")
    decoded = tuple(decode_response(response, form_id) for response in responses)
    identities = [response.response_id for response in decoded]
    if len(identities) != len(set(identities)):
        raise ValueError("page repeats a response")
    return ResponsePage(form_id, decoded, _string(resource.get("nextPageToken")))
