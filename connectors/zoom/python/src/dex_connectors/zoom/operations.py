# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoom meeting Queries and Mutations with Go-compatible outcomes."""

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    CREATE_MEETING_DEFINITION,
    GET_MEETING_DEFINITION,
    LIST_MEETINGS_DEFINITION,
    LIST_PAST_MEETING_PARTICIPANTS_DEFINITION,
    UPDATE_MEETING_DEFINITION,
)
from .client import Client, classify, failure
from .meeting import (
    decode_meeting_body,
    decode_meeting_page,
    decode_participant_page,
    page_size,
    settings_payload,
    start_instant,
    validate_page_token,
)
from .models import (
    CreateMeetingInput,
    GetMeetingInput,
    ListMeetingsInput,
    ListPastMeetingParticipantsInput,
    Meeting,
    MeetingPage,
    ParticipantPage,
    UpdatedMeeting,
    UpdateMeetingInput,
)


def _positive_meeting_id(meeting_id: int) -> None:
    if type(meeting_id) is not int or meeting_id < 1:
        raise ValueError("meetingId must be Zoom's positive numeric meeting ID")


def _listing_query(size: int, token: str) -> dict[str, str]:
    result = {"page_size": str(page_size(size))}
    validate_page_token(token)
    if token:
        result["next_page_token"] = token
    return result


def _topic(topic: str) -> None:
    if not isinstance(topic, str) or not topic.strip():
        raise ValueError("topic is required")
    if len(topic) > 200:
        raise ValueError("topic must be valid UTF-8 of at most 200 characters")


def _agenda(agenda: str) -> None:
    if not isinstance(agenda, str) or len(agenda) > 2000:
        raise ValueError("agenda must be valid UTF-8 of at most 2000 characters")


def _duration(value: int) -> None:
    if type(value) is not int or not 1 <= value <= 1440:
        raise ValueError("durationMinutes must be between 1 and 1440")


class ListMeetingsOperation:
    """List one page of the authorized user's meetings."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived listMeetings definition."""
        return LIST_MEETINGS_DEFINITION

    async def invoke(self, call: Call, input: ListMeetingsInput) -> QueryAttempt[MeetingPage]:
        """Classify a repeatable meeting page request."""
        operation = "listMeetings"
        empty = MeetingPage()
        try:
            if input.type not in (None, "", "upcoming", "scheduled", "live", "previous_meetings"):
                raise ValueError("type must be upcoming, scheduled, live, or previous_meetings")
            query = _listing_query(input.page_size, input.page_token)
            query["type"] = input.type or "upcoming"
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            return (
                QueryAttempt.retry(problem)
                if retryable
                else QueryAttempt.branch_result("defect", empty, Receipt(), problem)
            )
        assert credentials is not None
        exchange = await self.client.exchange(call, credentials, "GET", "/users/me/meetings", query)
        if exchange.error:
            kind = FailureKind.LOCAL_DEFECT if exchange.error == "notBuilt" else FailureKind.TRANSPORT
            return (
                QueryAttempt.branch_result(
                    "defect", empty, Receipt(), failure(operation, kind, "Zoom request could not be built")
                )
                if exchange.error == "notBuilt"
                else QueryAttempt.retry(failure(operation, kind, "Zoom could not be reached"))
            )
        route, problem, delay, code = classify(self.client, operation, exchange)
        if route == "retry":
            assert problem is not None
            return QueryAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, error_code=code if route != "invalid" else "")
        if route != "usable":
            return QueryAttempt.branch_result(
                "invalidResponse" if route == "invalid" else "providerRejected", empty, receipt, problem
            )
        try:
            page = decode_meeting_page(exchange.body)
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Zoom returned an unusable meeting page"),
            )
        return QueryAttempt.branch_result("listed", page, receipt)


class GetMeetingOperation:
    """Read one meeting by its numeric ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived getMeeting definition."""
        return GET_MEETING_DEFINITION

    async def invoke(self, call: Call, input: GetMeetingInput) -> QueryAttempt[Meeting]:
        """Classify a repeatable meeting read."""
        operation = "getMeeting"
        empty = Meeting()
        try:
            _positive_meeting_id(input.meeting_id)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            return (
                QueryAttempt.retry(problem)
                if retryable
                else QueryAttempt.branch_result("defect", empty, Receipt(), problem)
            )
        assert credentials is not None
        meeting_id = str(input.meeting_id)
        exchange = await self.client.exchange(call, credentials, "GET", "/meetings/" + meeting_id)
        if exchange.error:
            return QueryAttempt.retry(failure(operation, FailureKind.TRANSPORT, "Zoom could not be reached"))
        route, problem, delay, code = classify(self.client, operation, exchange)
        if route == "retry":
            assert problem is not None
            return QueryAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, meeting_id, code if route != "invalid" else "")
        if route == "notFound":
            return QueryAttempt.branch_result("notFound", Meeting(id=input.meeting_id), receipt, problem)
        if route != "usable":
            return QueryAttempt.branch_result(
                "invalidResponse" if route == "invalid" else "providerRejected", empty, receipt, problem
            )
        try:
            meeting = decode_meeting_body(exchange.body)
            if meeting.id != input.meeting_id:
                raise ValueError("meeting ID differs")
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Zoom returned an unusable meeting"),
            )
        return QueryAttempt.branch_result("found", meeting, receipt)


class ListPastMeetingParticipantsOperation:
    """List one page of people who joined a past meeting."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-derived participant listing definition."""
        return LIST_PAST_MEETING_PARTICIPANTS_DEFINITION

    async def invoke(
        self, call: Call, input: ListPastMeetingParticipantsInput
    ) -> QueryAttempt[ParticipantPage]:
        """Classify a repeatable participant page request."""
        operation = "listPastMeetingParticipants"
        empty = ParticipantPage()
        try:
            _positive_meeting_id(input.meeting_id)
            query = _listing_query(input.page_size, input.page_token)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            return (
                QueryAttempt.retry(problem)
                if retryable
                else QueryAttempt.branch_result("defect", empty, Receipt(), problem)
            )
        assert credentials is not None
        meeting_id = str(input.meeting_id)
        output = ParticipantPage(meeting_id=input.meeting_id)
        exchange = await self.client.exchange(
            call, credentials, "GET", "/past_meetings/" + meeting_id + "/participants", query
        )
        if exchange.error:
            return QueryAttempt.retry(failure(operation, FailureKind.TRANSPORT, "Zoom could not be reached"))
        route, problem, delay, code = classify(self.client, operation, exchange)
        if route == "retry":
            assert problem is not None
            return QueryAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, meeting_id, code if route != "invalid" else "")
        if route != "usable":
            branch = {"notFound": "notFound", "invalid": "invalidResponse"}.get(route, "providerRejected")
            return QueryAttempt.branch_result(branch, output, receipt, problem)
        try:
            page = decode_participant_page(exchange.body, input.meeting_id)
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Zoom returned an unusable participant page"),
            )
        return QueryAttempt.branch_result("listed", page, receipt)


class UpdateMeetingOperation:
    """Patch absolute meeting values so retries are safe."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-derived updateMeeting definition."""
        return UPDATE_MEETING_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateMeetingInput) -> IdempotencyKey:
        """Record the stable Call ID without sending a Zoom idempotency header."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateMeetingInput) -> MutationAttempt[UpdatedMeeting]:
        """Classify one repeatable PATCH attempt."""
        operation = "updateMeeting"
        empty = UpdatedMeeting()
        try:
            _positive_meeting_id(input.meeting_id)
            payload: dict[str, object] = {}
            if input.topic:
                _topic(input.topic)
                payload["topic"] = input.topic
            if input.agenda is not None:
                _agenda(input.agenda)
                payload["agenda"] = input.agenda
            if input.start_time:
                instant = start_instant(input.start_time, input.time_zone)
                if instant <= self.client.now():
                    raise ValueError(
                        "startTime is not in the future, and Zoom would replace it with the current time"
                    )
                payload["start_time"] = instant.strftime("%Y-%m-%dT%H:%M:%SZ")
                payload["timezone"] = input.time_zone
            elif input.time_zone:
                raise ValueError(
                    "timeZone requires startTime, so the instant and its display zone change together"
                )
            if input.duration_minutes:
                _duration(input.duration_minutes)
                payload["duration"] = input.duration_minutes
            settings = settings_payload(input.settings)
            if settings:
                payload["settings"] = settings
            if not payload:
                raise ValueError("updateMeeting needs at least one change")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            return (
                MutationAttempt.retry(problem)
                if retryable
                else MutationAttempt.branch_result("defect", empty, Receipt(), problem)
            )
        assert credentials is not None
        meeting_id = str(input.meeting_id)
        output = UpdatedMeeting(input.meeting_id)
        exchange = await self.client.exchange(
            call, credentials, "PATCH", "/meetings/" + meeting_id, payload=payload
        )
        if exchange.error:
            return MutationAttempt.retry(
                failure(operation, FailureKind.TRANSPORT, "Zoom could not be reached")
            )
        if 200 <= exchange.status < 300:
            return MutationAttempt.branch_result("updated", output, self.client.receipt(call, meeting_id))
        route, problem, delay, code = classify(self.client, operation, exchange)
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        branch = "notFound" if route == "notFound" else "providerRejected"
        return MutationAttempt.branch_result(
            branch, output, self.client.receipt(call, meeting_id, code), problem
        )


class CreateMeetingOperation:
    """Create one meeting behind the durable single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-derived createMeeting definition."""
        return CREATE_MEETING_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateMeetingInput) -> IdempotencyKey:
        """Record the stable Call ID; Zoom does not deduplicate create calls."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateMeetingInput) -> MutationAttempt[Meeting]:
        """Dispatch at most once and classify an unconfirmed outcome as uncertain."""
        operation = "createMeeting"
        empty = Meeting()
        try:
            _topic(input.topic)
            instant = start_instant(input.start_time, input.time_zone)
            _duration(input.duration_minutes)
            _agenda(input.agenda)
            settings = settings_payload(input.settings)
            payload: dict[str, object] = {
                "topic": input.topic,
                "type": 2,
                "start_time": instant.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "duration": input.duration_minutes,
                "timezone": input.time_zone,
            }
            if input.agenda:
                payload["agenda"] = input.agenda
            if settings:
                payload["settings"] = settings
            requested = Meeting(
                topic=input.topic,
                type=2,
                start_time=instant.strftime("%Y-%m-%dT%H:%M:%SZ"),
                duration_minutes=input.duration_minutes,
                time_zone=input.time_zone,
                agenda=input.agenda,
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            fence = Fence(call, CREATE_MEETING_DEFINITION.step_defaults.execute_method_timeout)
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    operation, FailureKind.LOCAL_DEFECT, "Zoom create dispatch fence could not be initialized"
                ),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                requested,
                failure(operation, FailureKind.TRANSPORT, "the Zoom create checkpoint could not be trusted"),
                self.client.receipt(call),
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            marker = decision.marker
            if marker is None or marker.call_id != call.id or not marker.provider_id:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "an earlier Zoom create may have succeeded without a confirmed meeting ID",
                    ),
                    self.client.receipt(call),
                )
            return await self._read_back(call, requested, marker.provider_id)
        if instant <= self.client.now():
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation, FailureKind.AVAILABILITY, "Zoom create not-sent checkpoint was unavailable"
                    )
                )
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "startTime is not in the future, and Zoom would replace it with the current time",
                ),
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Zoom credentials and the not-sent checkpoint were unavailable",
                    )
                )
            return (
                MutationAttempt.retry(problem)
                if retryable
                else MutationAttempt.branch_result("defect", empty, Receipt(), problem)
            )
        assert credentials is not None
        try:
            exchange = await fence.dispatch(
                lambda _deadline: self.client.exchange(
                    call, credentials, "POST", "/users/me/meetings", payload=payload, fenced=True
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Zoom create was not sent before the attempt deadline or checkpoint",
                )
            )
        except (TimeoutError, DispatchNotAllowedError):
            return MutationAttempt.uncertain(
                requested,
                failure(operation, FailureKind.TRANSPORT, "Zoom meeting outcome is unknown"),
                self.client.receipt(call),
            )
        if exchange.error == "notBuilt":
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "Zoom request could not be built"),
            )
        if exchange.error:
            if not exchange.is_dispatched:
                try:
                    await fence.record_can_resend()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            "Zoom was not reached but the resend checkpoint failed",
                        )
                    )
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "Zoom could not be reached, so no meeting was created",
                    )
                )
            return MutationAttempt.uncertain(
                requested,
                failure(operation, FailureKind.TRANSPORT, "Zoom meeting outcome is unknown"),
                self.client.receipt(call),
            )
        if 200 <= exchange.status < 300:
            if exchange.is_body_too_large:
                problem = failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Zoom accepted the request but its response exceeds the configured size limit",
                )
            elif exchange.has_body_read_failed:
                problem = failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Zoom accepted the request but its response was interrupted",
                )
            elif exchange.reflects_secret:
                problem = failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Zoom accepted the request but its response contained the connection credential",
                )
            else:
                try:
                    meeting = decode_meeting_body(exchange.body)
                except ValueError:
                    problem = failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Zoom accepted the request but returned an unusable meeting",
                    )
                else:
                    try:
                        await fence.record_provider_id(str(meeting.id))
                    except Exception:
                        pass  # A lost checkpoint cannot change a known successful response.
                    return MutationAttempt.branch_result(
                        "created", meeting, self.client.receipt(call, str(meeting.id))
                    )
            return MutationAttempt.uncertain(requested, problem, self.client.receipt(call))
        route, problem, delay, code = classify(self.client, operation, exchange)
        assert problem is not None
        receipt = self.client.receipt(call, error_code=code)
        if exchange.status == 401:
            resolver = getattr(self.client.credentials, "resolve_after_rejection", None)
            if resolver is None:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    requested,
                    receipt,
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "Zoom rejected the connection credential; reconnect the connection",
                    ),
                )
            try:
                replacement = await resolver(call)
                replaced = replacement.access_token.reveal() != credentials.access_token.reveal()
            except Exception:
                replaced = False
            if replaced:
                try:
                    await fence.record_can_resend()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            "Zoom rejected the credential but the resend checkpoint failed",
                        )
                    )
                return MutationAttempt.retry(problem)
            return MutationAttempt.branch_result(
                "providerRejected",
                requested,
                receipt,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Zoom connection requires reauthorization; reconnect the connection",
                ),
            )
        if exchange.status == 429 and route == "rejected":
            return MutationAttempt.branch_result("providerRejected", requested, receipt, problem)
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Zoom rejected the create but the resend checkpoint failed",
                    )
                )
            return MutationAttempt.retry(problem, delay)
        if exchange.status == 409:
            return MutationAttempt.uncertain(requested, problem, receipt)
        if 400 <= exchange.status < 500 and exchange.status != 408:
            return MutationAttempt.branch_result("providerRejected", requested, receipt, problem)
        return MutationAttempt.uncertain(requested, problem, receipt)

    async def _read_back(self, call: Call, requested: Meeting, provider_id: str) -> MutationAttempt[Meeting]:
        """Confirm a prior accepted ID without another create request."""
        operation = "createMeeting"
        try:
            meeting_id = int(provider_id)
            _positive_meeting_id(meeting_id)
        except ValueError:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    operation, FailureKind.PROTOCOL, "the accepted Zoom meeting ID could not be read back"
                ),
                self.client.receipt(call, provider_id),
            )
        credentials, problem, retryable = await self.client.resolve(call, operation)
        if problem:
            return (
                MutationAttempt.retry(problem)
                if retryable
                else MutationAttempt.uncertain(requested, problem, self.client.receipt(call, provider_id))
            )
        assert credentials is not None
        exchange = await self.client.exchange(call, credentials, "GET", "/meetings/" + provider_id)
        if exchange.error:
            return MutationAttempt.retry(
                failure(operation, FailureKind.TRANSPORT, "Zoom meeting read-back could not be reached")
            )
        route, problem, delay, _ = classify(self.client, operation, exchange)
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        if route == "usable":
            try:
                meeting = decode_meeting_body(exchange.body)
                if meeting.id == meeting_id:
                    return MutationAttempt.branch_result(
                        "created", meeting, self.client.receipt(call, provider_id)
                    )
            except ValueError:
                pass
        return MutationAttempt.uncertain(
            requested,
            failure(operation, FailureKind.PROTOCOL, "Zoom could not confirm the accepted meeting"),
            self.client.receipt(call, provider_id),
        )
