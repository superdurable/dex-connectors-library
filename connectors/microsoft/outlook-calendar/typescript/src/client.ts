// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch,
  newMutationRetry,
  newQueryBranch,
  newQueryRetry,
  zeroReceipt,
  type Call,
  type CredentialRefreshDriver,
  type Mutation,
  type MutationAttempt,
  type Query,
  type QueryAttempt,
  type Failure,
} from "@superdurable/connectors-sdk";
import { ManifestCredentialRefreshDriver } from "@superdurable/connectors-sdk/oauthtoken";
import { Authorization, CredentialFields } from "./generated/refresh.js";
import {
  CreateEventDefinition,
  GetEventDefinition,
  ListEventsDefinition,
  QueryFreeBusyDefinition,
  UpdateEventDefinition,
} from "./generated/definitions.js";
import type {
  Config,
  CredentialSource,
  Credentials,
  CreateEventInput,
  CreateEventOutput,
  Event,
  GetEventInput,
  ListEventsInput,
  ListEventsOutput,
  QueryFreeBusyInput,
  QueryFreeBusyOutput,
  ScheduleAvailability,
  ScheduleItem,
  UpdateEventInput,
  UpdateEventOutput,
} from "./generated/models.js";
import {
  CreateEventInputSchema,
  GetEventInputSchema,
  ListEventsInputSchema,
  QueryFreeBusyInputSchema,
  UpdateEventInputSchema,
} from "./generated/models.js";
import {
  GraphHTTP,
  RequestFailure,
  calendarPath,
  classification,
  decodeJSON,
  eventSelect,
  failure,
  listSelect,
  marker,
  object,
  pathEscape,
  receipt,
  string,
  validateNextLink,
  type GraphResponse,
  type Transport,
} from "./http.js";
import {
  displayZone,
  emptyCreate,
  emptyEvent,
  emptyUpdate,
  encodeAttendees,
  event,
  graphBoundary,
  graphUTC,
  instant,
  returnedBoundary,
  validateAddress,
  validateAttendees,
  validateCalendarId,
  validateEventId,
  validateEventTimes,
  windowTimes,
} from "./event.js";

/** Testable transport, clock, and generated OAuth driver. */
export interface ClientOptions {
  transport?: Transport;
  now?: () => Date;
  baseURL?: string;
  refreshDriver?: CredentialRefreshDriver<Credentials>;
}
/** Authenticated Outlook Calendar provider operations. */
export class Client {
  readonly http: GraphHTTP;
  /** Validate configuration and inject host-managed credentials. */
  constructor(
    config: Config,
    credentials: CredentialSource,
    options: ClientOptions = {},
  ) {
    const driver =
      options.refreshDriver ??
      new ManifestCredentialRefreshDriver<Credentials>({
        providerName: "Microsoft",
        auth: Authorization,
        credentialFields: CredentialFields,
        settings: { tenantId: config.tenantId ?? "" },
      });
    this.http = new GraphHTTP(
      config,
      credentials,
      driver,
      options.transport,
      options.baseURL,
      options.now,
    );
  }
  /** Bind listEvents. */ listEvents(): ListEventsOperation {
    return new ListEventsOperation(this.http);
  }
  /** Bind getEvent. */ getEvent(): GetEventOperation {
    return new GetEventOperation(this.http);
  }
  /** Bind createEvent. */ createEvent(): CreateEventOperation {
    return new CreateEventOperation(this.http);
  }
  /** Bind updateEvent. */ updateEvent(): UpdateEventOperation {
    return new UpdateEventOperation(this.http);
  }
  /** Bind queryFreeBusy. */ queryFreeBusy(): QueryFreeBusyOperation {
    return new QueryFreeBusyOperation(this.http);
  }
}
function failedQuery<T>(
  op: string,
  error: RequestFailure,
  empty: T,
  call: Call,
  http: GraphHTTP,
): QueryAttempt<T> {
  if (error.kind === "RESPONSE_TOO_LARGE")
    return newQueryBranch(
      "invalidResponse",
      empty,
      receipt(call, error.response, "", http.now),
      failure(op, error.kind, error.message),
    );
  if (error.kind === "LOCAL_DEFECT")
    return newQueryBranch(
      "defect",
      empty,
      zeroReceipt(),
      failure(op, error.kind, error.message),
    );
  return newQueryRetry(failure(op, "AVAILABILITY", "provider is unavailable"));
}
function failedMutation<T>(
  op: string,
  error: RequestFailure,
  empty: T,
  call: Call,
  http: GraphHTTP,
): MutationAttempt<T> {
  if (error.kind === "RESPONSE_TOO_LARGE")
    return newMutationBranch(
      "invalidResponse",
      empty,
      receipt(call, error.response, "", http.now),
      failure(op, error.kind, error.message),
    );
  if (error.kind === "LOCAL_DEFECT")
    return newMutationBranch(
      "defect",
      empty,
      zeroReceipt(),
      failure(op, error.kind, error.message),
    );
  return newMutationRetry(
    failure(
      op,
      "TRANSPORT",
      op === "createEvent"
        ? "create outcome is unknown; the retry looks the idempotency key up first"
        : "update outcome is unknown; the retry rereads the event first",
    ),
  );
}
function failedValidation<T>(
  op: string,
  error: unknown,
  empty: T,
): QueryAttempt<T> {
  const source = error instanceof RequestFailure ? error.kind : "VALIDATION";
  return newQueryBranch(
    "defect",
    empty,
    zeroReceipt(),
    failure(
      op,
      source,
      error instanceof Error ? error.message : "invalid input",
    ),
  );
}
function classifiedQuery<T>(
  op: string,
  response: GraphResponse,
  empty: T,
  call: Call,
  http: GraphHTTP,
): QueryAttempt<T> {
  const result = classification(response),
    problem = failure(op, result.kind, result.message);
  return result.retry
    ? newQueryRetry(problem, result.delay)
    : newQueryBranch(
        "providerRejected",
        empty,
        receipt(call, response, "", http.now),
        problem,
      );
}
function classifiedMutation<T>(
  op: string,
  response: GraphResponse,
  empty: T,
  call: Call,
  http: GraphHTTP,
): MutationAttempt<T> {
  const result = classification(response),
    problem = failure(op, result.kind, result.message);
  return result.retry
    ? newMutationRetry(problem, result.delay)
    : newMutationBranch(
        "providerRejected",
        empty,
        receipt(call, response, "", http.now),
        problem,
      );
}
async function readEvent(
  http: GraphHTTP,
  call: Call,
  credentials: Credentials,
  root: string,
  eventId: string,
  display: string,
  operation: string,
): Promise<{
  outcome: string;
  value: Event;
  response: GraphResponse;
  problem?: Failure;
  delay: number;
}> {
  let response: GraphResponse;
  try {
    response = await http.send(
      call,
      credentials,
      "GET",
      root + "/events/" + pathEscape(eventId),
      { $select: eventSelect },
    );
  } catch (error) {
    if (!(error instanceof RequestFailure)) throw error;
    return {
      outcome:
        error.kind === "RESPONSE_TOO_LARGE"
          ? "invalidResponse"
          : error.kind === "LOCAL_DEFECT"
            ? "defect"
            : "retry",
      value: emptyEvent(),
      response: error.response,
      problem: failure(
        operation,
        error.kind === "RESPONSE_TOO_LARGE" || error.kind === "LOCAL_DEFECT"
          ? error.kind
          : "AVAILABILITY",
        error.kind === "RESPONSE_TOO_LARGE" || error.kind === "LOCAL_DEFECT"
          ? error.message
          : "provider is unavailable",
      ),
      delay: 0,
    };
  }
  if ([404, 410].includes(response.status))
    return {
      outcome: "notFound",
      value: emptyEvent(),
      response,
      problem: failure(operation, "NOT_FOUND", "event was not found"),
      delay: 0,
    };
  if (response.status < 200 || response.status >= 300) {
    const result = classification(response);
    return {
      outcome: result.retry ? "retry" : "providerRejected",
      value: emptyEvent(),
      response,
      problem: failure(operation, result.kind, result.message),
      delay: result.delay,
    };
  }
  try {
    return {
      outcome: "found",
      value: event(decodeJSON(response.body), "", display),
      response,
      delay: 0,
    };
  } catch (error) {
    return {
      outcome: "invalidResponse",
      value: emptyEvent(),
      response,
      problem: failure(
        operation,
        "PROTOCOL",
        "provider returned an invalid event: " +
          (error instanceof Error ? error.message : ""),
      ),
      delay: 0,
    };
  }
}
interface LookupResult {
  outcome: string;
  value: Event;
  duplicates: string[];
  response: GraphResponse;
  problem?: Failure;
  delay: number;
}
async function lookup(
  http: GraphHTTP,
  call: Call,
  credentials: Credentials,
  root: string,
  key: string,
  calendarId: string,
  display: string,
  operation: string,
): Promise<LookupResult> {
  const filter = `singleValueExtendedProperties/Any(ep: ep/id eq '${marker}' and ep/value eq '${key}')`;
  let response: GraphResponse;
  try {
    response = await http.send(call, credentials, "GET", root + "/events", {
      $filter: filter,
      $select: eventSelect,
      $top: "10",
    });
  } catch (error) {
    if (!(error instanceof RequestFailure)) throw error;
    return {
      outcome:
        error.kind === "RESPONSE_TOO_LARGE"
          ? "invalidResponse"
          : error.kind === "LOCAL_DEFECT"
            ? "defect"
            : "retry",
      value: emptyEvent(),
      duplicates: [],
      response: error.response,
      problem: failure(
        operation,
        error.kind === "RESPONSE_TOO_LARGE" || error.kind === "LOCAL_DEFECT"
          ? error.kind
          : "AVAILABILITY",
        error.kind === "RESPONSE_TOO_LARGE" || error.kind === "LOCAL_DEFECT"
          ? error.message
          : "provider is unavailable",
      ),
      delay: 0,
    };
  }
  if (response.status < 200 || response.status >= 300) {
    const result = classification(response);
    return {
      outcome: result.retry ? "retry" : "providerRejected",
      value: emptyEvent(),
      duplicates: [],
      response,
      problem: failure(operation, result.kind, result.message),
      delay: result.delay,
    };
  }
  try {
    const value = object(decodeJSON(response.body)).value;
    if (!Array.isArray(value))
      throw new TypeError("event lookup is not an array");
    if (!value.length)
      return {
        outcome: "missing",
        value: emptyEvent(),
        duplicates: [],
        response,
        problem: failure(
          operation,
          "NOT_FOUND",
          "no event carries the idempotency key",
        ),
        delay: 0,
      };
    const events = value.map((item) => event(item, calendarId, display));
    return {
      outcome: "found",
      value: events[0]!,
      duplicates: events.slice(1).map((item) => item.id),
      response,
      delay: 0,
    };
  } catch (error) {
    return {
      outcome: "invalidResponse",
      value: emptyEvent(),
      duplicates: [],
      response,
      problem: failure(
        operation,
        "PROTOCOL",
        "provider returned an invalid event: " +
          (error instanceof Error ? error.message : ""),
      ),
      delay: 0,
    };
  }
}
/** One bounded calendar view page. */
export class ListEventsOperation
  implements Query<ListEventsInput, ListEventsOutput>
{
  constructor(private readonly http: GraphHTTP) {}
  definition() {
    return ListEventsDefinition;
  }
  async invoke(
    call: Call,
    input: ListEventsInput,
  ): Promise<QueryAttempt<ListEventsOutput>> {
    const op = "listEvents",
      empty = emptyList();
    let display: string, credentials: Credentials, root: string;
    try {
      ListEventsInputSchema.parse(input);
      validateCalendarId(input.calendarId ?? "");
      const [start, end] = windowTimes(input.timeMin, input.timeMax);
      if (end - start > 366 * 86400_000)
        throw new TypeError(
          "the timeMin to timeMax window cannot exceed 366 days",
        );
      display = displayZone(input.timeZone);
      if (input.pageToken) validateNextLink(input.pageToken);
      if (
        input.pageSize !== undefined &&
        (!Number.isSafeInteger(input.pageSize) ||
          input.pageSize < 0 ||
          input.pageSize > 250)
      )
        throw new TypeError(
          "pageSize must be between 1 and 250, or zero for the default",
        );
      ({ credentials, root } = await this.http.resolve(call));
    } catch (error) {
      return failedValidation(op, error, empty);
    }
    let response: GraphResponse;
    try {
      response = await this.http.send(
        call,
        credentials,
        "GET",
        calendarPath(root, input.calendarId ?? "") + "/calendarView",
        input.pageToken
          ? undefined
          : {
              startDateTime: input.timeMin,
              endDateTime: input.timeMax,
              $top: String(input.pageSize || 50),
              $select: listSelect,
            },
        undefined,
        "",
        input.pageToken ?? "",
      );
    } catch (error) {
      if (error instanceof RequestFailure)
        return failedQuery(op, error, empty, call, this.http);
      throw error;
    }
    if ([404, 410].includes(response.status))
      return newQueryBranch(
        "notFound",
        empty,
        receipt(call, response, "", this.http.now),
        failure(op, "NOT_FOUND", "calendar was not found"),
      );
    if (response.status < 200 || response.status >= 300)
      return classifiedQuery(op, response, empty, call, this.http);
    try {
      const page = object(decodeJSON(response.body));
      if (!Array.isArray(page.value))
        throw new TypeError(
          "event page is not a JSON object with a value array",
        );
      const next = string(page["@odata.nextLink"]);
      if (next) validateNextLink(next);
      const events = page.value
        .map((item) => event(item, input.calendarId ?? "", display))
        .sort(
          (left, right) =>
            instant("dateTime", left.start.dateTime) -
            instant("dateTime", right.start.dateTime),
        );
      return newQueryBranch(
        "listed",
        {
          ...(input.calendarId ? { calendarId: input.calendarId } : {}),
          timeMin: input.timeMin,
          timeMax: input.timeMax,
          timeZone: display,
          events,
          ...(next ? { nextPageToken: next } : {}),
        },
        receipt(call, response, "", this.http.now),
      );
    } catch (error) {
      return newQueryBranch(
        "invalidResponse",
        empty,
        receipt(call, response, "", this.http.now),
        failure(
          op,
          "PROTOCOL",
          "provider returned an invalid event page: " +
            (error instanceof Error ? error.message : ""),
        ),
      );
    }
  }
}
function emptyList(): ListEventsOutput {
  return { timeMin: "", timeMax: "", timeZone: "", events: [] };
}
/** Read one event by Graph ID. */
export class GetEventOperation implements Query<GetEventInput, Event> {
  constructor(private readonly http: GraphHTTP) {}
  definition() {
    return GetEventDefinition;
  }
  async invoke(call: Call, input: GetEventInput): Promise<QueryAttempt<Event>> {
    const op = "getEvent";
    let display: string, credentials: Credentials, root: string;
    try {
      GetEventInputSchema.parse(input);
      validateEventId(input.eventId);
      display = displayZone(input.timeZone);
      ({ credentials, root } = await this.http.resolve(call));
    } catch (error) {
      return failedValidation(op, error, emptyEvent());
    }
    const result = await readEvent(
      this.http,
      call,
      credentials,
      root,
      input.eventId,
      display,
      op,
    );
    if (result.outcome === "retry")
      return newQueryRetry(
        result.problem ??
          failure(op, "AVAILABILITY", "provider is unavailable"),
        result.delay,
      );
    return newQueryBranch(
      result.outcome,
      result.outcome === "found" ? result.value : emptyEvent(),
      result.outcome === "defect"
        ? zeroReceipt()
        : receipt(call, result.response, result.value.id, this.http.now),
      result.problem,
    );
  }
}
function emptyFreeBusy(): QueryFreeBusyOutput {
  return {
    timeMin: "",
    timeMax: "",
    timeZone: "",
    availabilityViewInterval: 0,
    schedules: [],
  };
}
function availability(
  scheduleId: string,
  raw: unknown,
  display: string,
): ScheduleAvailability {
  const value = object(raw);
  const items: ScheduleItem[] = [];
  let busy = false,
    known = true,
    reason = "";
  for (const entry of Array.isArray(value.scheduleItems)
    ? value.scheduleItems
    : []) {
    try {
      const item = object(entry),
        start = returnedBoundary("scheduleItem.start", item.start, display),
        end = returnedBoundary("scheduleItem.end", item.end, display);
      if (end.epoch < start.epoch) throw new TypeError();
      const status = string(item.status) || "unknown";
      items.push({
        start: start.value.dateTime,
        end: end.value.dateTime,
        status,
      });
      if (["tentative", "busy", "oof"].includes(status)) busy = true;
      else if (!["free", "workingElsewhere"].includes(status) && known) {
        known = false;
        reason = "unknownStatus";
      }
    } catch {
      if (value.error !== undefined && value.error !== null) continue;
      throw new TypeError(
        `schedule ${scheduleId} has an item without two ordered times`,
      );
    }
  }
  const view = string(value.availabilityView);
  if (/[123]/.test(view)) busy = true;
  if (!view || /[^01234]/.test(view)) {
    if (known) {
      known = false;
      reason = "unreadableAvailabilityView";
    }
  }
  let code = "";
  if (value.error !== undefined && value.error !== null) {
    known = false;
    reason = "error";
    if (value.error && typeof value.error === "object") {
      const candidate = string(object(value.error).responseCode);
      if (/^[A-Za-z][A-Za-z0-9]{0,63}$/.test(candidate)) code = candidate;
    }
  }
  return {
    scheduleId,
    isBusy: busy,
    isAvailabilityKnown: known,
    ...(reason ? { unknownReason: reason } : {}),
    ...(code ? { errorResponseCode: code } : {}),
    items,
    availabilityView: view,
  };
}
/** Query Graph getSchedule for bounded availability. */
export class QueryFreeBusyOperation
  implements Query<QueryFreeBusyInput, QueryFreeBusyOutput>
{
  constructor(private readonly http: GraphHTTP) {}
  definition() {
    return QueryFreeBusyDefinition;
  }
  async invoke(
    call: Call,
    input: QueryFreeBusyInput,
  ): Promise<QueryAttempt<QueryFreeBusyOutput>> {
    const op = "queryFreeBusy",
      empty = emptyFreeBusy();
    let first: number,
      last: number,
      display: string,
      interval: number,
      credentials: Credentials,
      root: string;
    try {
      QueryFreeBusyInputSchema.parse(input);
      if (
        !Array.isArray(input.schedules) ||
        input.schedules.length < 1 ||
        input.schedules.length > 20
      )
        throw new TypeError("schedules must list 1 to 20 addresses");
      const seen = new Set<string>();
      for (const schedule of input.schedules) {
        validateAddress(schedule);
        const lower = schedule.toLowerCase();
        if (seen.has(lower)) throw new TypeError("schedule is duplicated");
        seen.add(lower);
      }
      [first, last] = windowTimes(input.timeMin, input.timeMax);
      if (last - first >= 62 * 86400_000)
        throw new TypeError(
          "the timeMin to timeMax window must be shorter than 62 days",
        );
      interval = input.availabilityViewInterval || 30;
      if (!Number.isSafeInteger(interval) || interval < 5 || interval > 1440)
        throw new TypeError(
          "availabilityViewInterval must be between 5 and 1440 minutes, or zero for the default",
        );
      display = displayZone(input.timeZone);
      ({ credentials, root } = await this.http.resolve(call));
    } catch (error) {
      return failedValidation(op, error, empty);
    }
    let response: GraphResponse;
    try {
      response = await this.http.send(
        call,
        credentials,
        "POST",
        root + "/calendar/getSchedule",
        undefined,
        {
          schedules: input.schedules,
          startTime: graphUTC(first),
          endTime: graphUTC(last),
          availabilityViewInterval: interval,
        },
      );
    } catch (error) {
      if (error instanceof RequestFailure)
        return failedQuery(op, error, empty, call, this.http);
      throw error;
    }
    if (response.status < 200 || response.status >= 300)
      return classifiedQuery(op, response, empty, call, this.http);
    try {
      const resource = object(decodeJSON(response.body));
      if (!Array.isArray(resource.value))
        throw new TypeError(
          "schedule response is not a JSON object with a value array",
        );
      const returned = new Map(
        resource.value.map((raw) => {
          const item = object(raw);
          return [string(item.scheduleId).toLowerCase(), item] as const;
        }),
      );
      const schedules = input.schedules!.map((schedule) =>
        returned.has(schedule.toLowerCase())
          ? availability(
              schedule,
              returned.get(schedule.toLowerCase()),
              display,
            )
          : {
              scheduleId: schedule,
              isBusy: false,
              isAvailabilityKnown: false,
              unknownReason: "missing",
              items: [],
              availabilityView: "",
            },
      );
      const output: QueryFreeBusyOutput = {
        timeMin: input.timeMin,
        timeMax: input.timeMax,
        timeZone: display,
        availabilityViewInterval: interval,
        schedules,
      };
      if (schedules.some((item) => !item.isAvailabilityKnown))
        return newQueryBranch(
          "incomplete",
          output,
          receipt(call, response, "", this.http.now),
          failure(
            op,
            "PROVIDER_REJECTION",
            "provider could not report availability for every schedule",
          ),
        );
      return newQueryBranch(
        "queried",
        output,
        receipt(call, response, "", this.http.now),
      );
    } catch (error) {
      return newQueryBranch(
        "invalidResponse",
        empty,
        receipt(call, response, "", this.http.now),
        failure(
          op,
          "PROTOCOL",
          "provider returned an invalid schedule response: " +
            (error instanceof Error ? error.message : ""),
        ),
      );
    }
  }
}
function createPayload(
  input: CreateEventInput,
  key: string,
): Record<string, unknown> {
  return {
    subject: input.subject,
    start: graphBoundary(input.start),
    end: graphBoundary(input.end),
    transactionId: key,
    singleValueExtendedProperties: [{ id: marker, value: key }],
    ...(input.isAllDay ? { isAllDay: true } : {}),
    ...(input.body
      ? { body: { contentType: "text", content: input.body } }
      : {}),
    ...(input.location ? { location: { displayName: input.location } } : {}),
    ...(input.attendees?.length
      ? { attendees: encodeAttendees(input.attendees) }
      : {}),
    ...(input.requestOnlineMeeting
      ? { isOnlineMeeting: true, onlineMeetingProvider: "teamsForBusiness" }
      : {}),
  };
}
function existingCreate(
  result: LookupResult,
  call: Call,
  http: GraphHTTP,
): MutationAttempt<CreateEventOutput> {
  const op = "createEvent",
    empty = emptyCreate();
  const resultReceipt = receipt(
    call,
    result.response,
    result.value.id,
    http.now,
  );
  if (result.outcome === "found")
    return newMutationBranch(
      "created",
      {
        event: result.value,
        wasAlreadyCreated: true,
        ...(result.duplicates.length
          ? { duplicateEventIds: result.duplicates }
          : {}),
      },
      resultReceipt,
    );
  if (result.outcome === "retry")
    return newMutationRetry(
      result.problem ?? failure(op, "AVAILABILITY", "provider is unavailable"),
      result.delay,
    );
  if (result.outcome === "invalidResponse")
    return newMutationBranch(
      "invalidResponse",
      empty,
      resultReceipt,
      result.problem,
    );
  if (result.outcome === "defect")
    return newMutationBranch("defect", empty, zeroReceipt(), result.problem);
  return newMutationBranch(
    "providerRejected",
    empty,
    resultReceipt,
    result.problem,
  );
}
/** Create an event using Graph transaction and extended property idempotency. */
export class CreateEventOperation
  implements Mutation<CreateEventInput, CreateEventOutput>
{
  constructor(private readonly http: GraphHTTP) {}
  definition() {
    return CreateEventDefinition;
  }
  idempotencyKey(callId: string, input: CreateEventInput): string {
    return input.transactionId || callId;
  }
  async invoke(
    call: Call,
    input: CreateEventInput,
  ): Promise<MutationAttempt<CreateEventOutput>> {
    const op = "createEvent",
      empty = emptyCreate(),
      key = call.idempotencyKey ?? "";
    let display: string, credentials: Credentials, root: string;
    try {
      CreateEventInputSchema.parse(input);
      validateCalendarId(input.calendarId ?? "");
      if (!/^[A-Za-z0-9._-]{1,64}$/.test(key))
        throw new TypeError(
          "transactionId must be 1 to 64 letters, digits, dots, hyphens, or underscores",
        );
      if (!input.subject?.trim()) throw new TypeError("subject is required");
      validateEventTimes(input.start, input.end, input.isAllDay ?? false);
      validateAttendees(input.attendees ?? []);
      display = displayZone(input.start.timeZone);
      ({ credentials, root } = await this.http.resolve(call));
    } catch (error) {
      return failedValidation(op, error, empty);
    }
    const previous = await lookup(
      this.http,
      call,
      credentials,
      root,
      key,
      input.calendarId ?? "",
      display,
      op,
    );
    if (previous.outcome !== "missing")
      return existingCreate(previous, call, this.http);
    let response: GraphResponse;
    try {
      response = await this.http.send(
        call,
        credentials,
        "POST",
        calendarPath(root, input.calendarId ?? "") + "/events",
        undefined,
        createPayload(input, key),
      );
    } catch (error) {
      if (error instanceof RequestFailure)
        return failedMutation(op, error, empty, call, this.http);
      throw error;
    }
    const observed = receipt(call, response, "", this.http.now);
    if (response.status >= 200 && response.status < 300) {
      try {
        const created = event(
          decodeJSON(response.body),
          input.calendarId ?? "",
          display,
        );
        if (created.transactionId && created.transactionId !== key)
          throw new TypeError("another transactionId");
        return newMutationBranch(
          "created",
          { event: created, wasAlreadyCreated: false },
          receipt(call, response, created.id, this.http.now),
        );
      } catch (error) {
        const message =
          error instanceof Error && error.message === "another transactionId"
            ? "provider returned an event with another transactionId"
            : "provider returned an invalid event: " +
              (error instanceof Error ? error.message : "");
        return newMutationBranch(
          "invalidResponse",
          empty,
          observed,
          failure(op, "PROTOCOL", message),
        );
      }
    }
    if ([400, 409].includes(response.status)) {
      const again = await lookup(
        this.http,
        call,
        credentials,
        root,
        key,
        input.calendarId ?? "",
        display,
        op,
      );
      if (again.outcome !== "missing")
        return existingCreate(again, call, this.http);
      const result = classification(response);
      if (response.status === 409)
        return newMutationRetry(
          failure(op, result.kind, result.message),
          result.delay,
        );
      return newMutationBranch(
        "providerRejected",
        empty,
        observed,
        failure(op, result.kind, result.message),
      );
    }
    return classifiedMutation(op, response, empty, call, this.http);
  }
}
function updateApplied(current: Event, input: UpdateEventInput): boolean {
  if (input.subject !== undefined && current.subject !== input.subject)
    return false;
  if (
    input.body !== undefined &&
    (current.body ?? "").replace(/\r\n/g, "\n").trim() !==
      input.body.replace(/\r\n/g, "\n").trim()
  )
    return false;
  if (input.location !== undefined && (current.location ?? "") !== input.location)
    return false;
  if (
    input.start &&
    input.end &&
    (current.isAllDay !== (input.isAllDay ?? false) ||
      instant("dateTime", current.start.dateTime) !==
        instant("dateTime", input.start.dateTime) ||
      instant("dateTime", current.end.dateTime) !==
        instant("dateTime", input.end.dateTime))
  )
    return false;
  if (input.attendees !== undefined && input.attendees !== null) {
    const actual = (current.attendees ?? [])
      .map(
        (item) =>
          `${item.email.toLowerCase()}|${(item.type ?? "").toLowerCase()}`,
      )
      .sort();
    const desired = input.attendees
      .map(
        (item) =>
          `${item.email.toLowerCase()}|${item.isOptional ? "optional" : "required"}`,
      )
      .sort();
    if (JSON.stringify(actual) !== JSON.stringify(desired)) return false;
  }
  return true;
}
function updatePayload(input: UpdateEventInput): Record<string, unknown> {
  return {
    ...(input.subject !== undefined ? { subject: input.subject } : {}),
    ...(input.body !== undefined
      ? { body: { contentType: "text", content: input.body } }
      : {}),
    ...(input.location !== undefined
      ? { location: { displayName: input.location } }
      : {}),
    ...(input.start && input.end
      ? {
          start: graphBoundary(input.start),
          end: graphBoundary(input.end),
          isAllDay: input.isAllDay ?? false,
        }
      : {}),
    ...(input.attendees !== undefined && input.attendees !== null
      ? { attendees: encodeAttendees(input.attendees) }
      : {}),
  };
}
/** Compare an event and conditionally patch it with its ETag. */
export class UpdateEventOperation
  implements Mutation<UpdateEventInput, UpdateEventOutput>
{
  constructor(private readonly http: GraphHTTP) {}
  definition() {
    return UpdateEventDefinition;
  }
  idempotencyKey(callId: string): string {
    return callId;
  }
  async invoke(
    call: Call,
    input: UpdateEventInput,
  ): Promise<MutationAttempt<UpdateEventOutput>> {
    const op = "updateEvent",
      empty = emptyUpdate();
    let display: string, credentials: Credentials, root: string;
    try {
      UpdateEventInputSchema.parse(input);
      validateEventId(input.eventId);
      if (
        [
          input.subject,
          input.body,
          input.location,
          input.start,
          input.end,
          input.attendees,
        ].every((value) => value === undefined || value === null)
      )
        throw new TypeError(
          "set at least one of subject, body, location, start and end, or attendees",
        );
      if (input.subject !== undefined && !input.subject.trim())
        throw new TypeError("subject cannot be blank");
      if (Boolean(input.start) !== Boolean(input.end))
        throw new TypeError(
          "set start and end together so the event keeps one unambiguous time range",
        );
      if (input.isAllDay && !input.start)
        throw new TypeError(
          "isAllDay applies only together with start and end",
        );
      if (input.start && input.end)
        validateEventTimes(input.start, input.end, input.isAllDay ?? false);
      if (input.attendees) validateAttendees(input.attendees);
      display = displayZone(input.timeZone || input.start?.timeZone);
      ({ credentials, root } = await this.http.resolve(call));
    } catch (error) {
      return failedValidation(op, error, empty);
    }
    const current = await readEvent(
      this.http,
      call,
      credentials,
      root,
      input.eventId,
      display,
      op,
    );
    const readReceipt = receipt(
      call,
      current.response,
      input.eventId,
      this.http.now,
    );
    if (current.outcome === "retry")
      return newMutationRetry(
        current.problem ??
          failure(op, "AVAILABILITY", "provider is unavailable"),
        current.delay,
      );
    if (current.outcome !== "found")
      return newMutationBranch(
        ["notFound", "providerRejected", "invalidResponse"].includes(
          current.outcome,
        )
          ? current.outcome
          : "defect",
        empty,
        current.outcome === "defect" ? zeroReceipt() : readReceipt,
        current.problem,
      );
    if (current.value.isCancelled)
      return newMutationBranch(
        "notFound",
        empty,
        readReceipt,
        failure(op, "NOT_FOUND", "event is cancelled"),
      );
    if (updateApplied(current.value, input))
      return newMutationBranch(
        "updated",
        { event: current.value, wasAlreadyApplied: true },
        readReceipt,
      );
    let response: GraphResponse;
    try {
      response = await this.http.send(
        call,
        credentials,
        "PATCH",
        root + "/events/" + pathEscape(input.eventId),
        undefined,
        updatePayload(input),
        current.value.etag ?? "",
      );
    } catch (error) {
      if (error instanceof RequestFailure)
        return failedMutation(op, error, empty, call, this.http);
      throw error;
    }
    const observed = receipt(call, response, input.eventId, this.http.now);
    if ([409, 412].includes(response.status)) {
      const token = string(await Promise.resolve(decodeErrorCode(response)));
      return newMutationRetry(
        failure(
          op,
          "CONFLICT",
          "event changed after it was read" + (token ? ` (${token})` : ""),
        ),
      );
    }
    if ([404, 410].includes(response.status))
      return newMutationBranch(
        "notFound",
        empty,
        observed,
        failure(op, "NOT_FOUND", "event was not found"),
      );
    if (response.status < 200 || response.status >= 300)
      return classifiedMutation(op, response, empty, call, this.http);
    try {
      return newMutationBranch(
        "updated",
        {
          event: event(decodeJSON(response.body), "", display),
          wasAlreadyApplied: false,
        },
        observed,
      );
    } catch (error) {
      return newMutationBranch(
        "invalidResponse",
        empty,
        observed,
        failure(
          op,
          "PROTOCOL",
          "provider returned an invalid event: " +
            (error instanceof Error ? error.message : ""),
        ),
      );
    }
  }
}
function decodeErrorCode(response: GraphResponse): string {
  try {
    const code = string(object(object(decodeJSON(response.body)).error).code);
    return /^[A-Za-z][A-Za-z0-9]{0,63}$/.test(code) ? code : "";
  } catch {
    return "";
  }
}
