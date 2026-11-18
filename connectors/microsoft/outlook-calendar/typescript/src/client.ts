// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { CheckpointError, InsufficientTimeError, newFence } from "@superdurable/connectors-sdk/singledispatch";
import {
  definitionMaxSends,
  newMutationBranch,
  newMutationRetry,
  newMutationUncertain,
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
function failedValidation<T>(
  op: string,
  error: unknown,
  empty: T,
): QueryAttempt<T> {
  const source = error instanceof RequestFailure ? error.kind : "VALIDATION";
  // Only a non-terminal credential failure carries AVAILABILITY here.
  if (source === "AVAILABILITY")
    return newQueryRetry(failure(op, source, (error as Error).message));
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
    let display: string;
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
    } catch (error) {
      return failedValidation(op, error, empty);
    }
    const operationMaxSends = definitionMaxSends(CreateEventDefinition);
    const maxSends = operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
    let fence: Fence;
    try {
      fence = newFence(call, 30_000, { maxSends: operationMaxSends });
    } catch {
      return newMutationBranch("defect", empty, zeroReceipt(), failure(op, "LOCAL_DEFECT", "createEvent dispatch fence is unavailable"));
    }
    const decision = fence.decide();
    // An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
    if (decision.kind === "failClosed")
      return newMutationRetry(failure(op, "TRANSPORT", "an earlier attempt may have created the event, and its dispatch checkpoint is unreadable"));
    const priorSends = decision.priorSendUpperBound ?? 0;
    const providerId = decision.marker?.providerId ?? "";
    return new CreateEventAttempt(this.http, call, fence, input, key, display, {
      hasPossibleEarlierSend: decision.kind !== "sendAllowed" || priorSends > 0 || providerId !== "",
      canResendUnknownOutcome: priorSends + 1 < maxSends,
      isSendForbidden: decision.kind === "readBackRequired" || providerId !== "",
    }).run();
  }
}

type Fence = ReturnType<typeof newFence>;
/** A 2xx whose event ID could not be read is recorded so that no later attempt POSTs again. */
const acceptedEventProviderId = "accepted";
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** An allowlisted refusal status whose body carries no created item ID. */
function isDefiniteGraphRefusal(response: GraphResponse): boolean {
  if (!definiteRefusalStatuses.has(response.status)) return false;
  try {
    const document = JSON.parse(Buffer.from(response.body).toString("utf8")) as unknown;
    if (!document || typeof document !== "object" || Array.isArray(document)) return true;
    const id = (document as { id?: unknown }).id;
    return id === undefined || id === null || id === "";
  } catch {
    return true;
  }
}

/** One createEvent attempt's Fence decision and resolved connection. */
class CreateEventAttempt {
  private credentials?: Credentials;
  private root = "";
  constructor(
    private readonly http: GraphHTTP,
    private readonly call: Call,
    private readonly fence: Fence,
    private readonly input: CreateEventInput,
    private readonly key: string,
    private readonly display: string,
    private readonly state: { hasPossibleEarlierSend: boolean; canResendUnknownOutcome: boolean; isSendForbidden: boolean },
  ) {}

  /** Resolve credentials, read the marker back, and POST only when the cap allows. */
  async run(): Promise<MutationAttempt<CreateEventOutput>> {
    try {
      ({ credentials: this.credentials, root: this.root } = await this.http.resolve(this.call));
    } catch (error) {
      if (!(error instanceof RequestFailure)) throw error;
      return this.credentialFailure(error);
    }
    const decided = await this.lookUp();
    if (decided) return decided;
    if (this.state.isSendForbidden)
      return this.uncertain(failure("createEvent", "TRANSPORT", "an earlier attempt may have created the event, and the marker lookup cannot confirm it"));
    return this.send();
  }

  private async credentialFailure(error: RequestFailure): Promise<MutationAttempt<CreateEventOutput>> {
    const problem = failure("createEvent", error.kind, error.message);
    const isRetryable = error.kind === "AVAILABILITY";
    if (this.state.hasPossibleEarlierSend)
      return isRetryable ? newMutationRetry(problem) : this.uncertain(failure("createEvent", error.kind, "an earlier attempt may have created the event, and the connection credentials are unavailable to confirm it"));
    try { await this.fence.recordNotSent(); }
    catch { return newMutationRetry(failure("createEvent", "AVAILABILITY", "credentials and the no-send checkpoint are unavailable")); }
    return isRetryable ? newMutationRetry(problem) : newMutationBranch("defect", emptyCreate(), zeroReceipt(), problem);
  }

  private lookup(): Promise<LookupResult> {
    return lookup(this.http, this.call, this.credentials!, this.root, this.key, this.input.calendarId ?? "", this.display, "createEvent");
  }

  private async recordProviderId(providerId: string): Promise<boolean> {
    try { await this.fence.recordProviderId(providerId); return true; } catch { return false; }
  }

  private created(result: LookupResult | { value: Event; duplicates: string[]; response: GraphResponse }, isExisting: boolean): MutationAttempt<CreateEventOutput> {
    return newMutationBranch("created", {
      event: result.value, wasAlreadyCreated: isExisting,
      ...(result.duplicates.length ? { duplicateEventIds: result.duplicates } : {}),
    }, receipt(this.call, result.response, result.value.id, this.http.now));
  }

  private async lookUp(): Promise<MutationAttempt<CreateEventOutput> | undefined> {
    const result = await this.lookup();
    const problem = result.problem ?? failure("createEvent", "AVAILABILITY", "provider is unavailable");
    if (result.outcome === "missing") return undefined;
    if (result.outcome === "found") {
      await this.recordProviderId(result.value.id);
      return this.created(result, true);
    }
    if (["providerRejected", "notFound"].includes(result.outcome) && isDefiniteGraphRefusal(result.response)) {
      if (this.state.hasPossibleEarlierSend) return this.uncertain(problem, result.response);
      // Nothing was sent; a lost checkpoint leaves no claim to misreport.
      await this.fence.recordNotSent().catch(() => undefined);
      return newMutationBranch("providerRejected", emptyCreate(), receipt(this.call, result.response, "", this.http.now), problem);
    }
    if (result.outcome === "invalidResponse" && !this.state.hasPossibleEarlierSend) {
      // Nothing was sent yet, so an unusable lookup answer can end the Step without a duplicate.
      await this.fence.recordNotSent().catch(() => undefined);
      return newMutationBranch("invalidResponse", emptyCreate(), receipt(this.call, result.response, "", this.http.now), problem);
    }
    if (result.outcome === "defect") {
      if (this.state.hasPossibleEarlierSend) return this.uncertain(problem);
      await this.fence.recordNotSent().catch(() => undefined);
      return newMutationBranch("defect", emptyCreate(), zeroReceipt(), problem);
    }
    // A transient, unlisted, or unreadable lookup answer leaves the outcome unknown.
    return newMutationRetry(problem, result.delay);
  }

  private async send(): Promise<MutationAttempt<CreateEventOutput>> {
    const op = "createEvent";
    let response: GraphResponse;
    try {
      response = await this.fence.dispatch((signal) => this.http.send(this.call, this.credentials!, "POST",
        calendarPath(this.root, this.input.calendarId ?? "") + "/events", undefined, createPayload(this.input, this.key), "", "", signal));
    } catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError)
        return newMutationRetry(failure(op, "AVAILABILITY", "the event POST was not sent in this attempt"));
      if (!(error instanceof RequestFailure)) return this.resendOrUncertain(failure(op, "TRANSPORT", "create outcome is unknown; the retry looks the idempotency key up first"), 0);
      if (error.response.status >= 200 && error.response.status < 300) return this.acceptedWithoutEvent(error.response);
      if (error.kind === "LOCAL_DEFECT" && !this.state.hasPossibleEarlierSend)
        return newMutationBranch("defect", emptyCreate(), zeroReceipt(), failure(op, error.kind, error.message));
      return this.resendOrUncertain(failure(op, "TRANSPORT", "create outcome is unknown; the retry looks the idempotency key up first"), 0, error.response);
    }
    if (response.status === 429 || response.status === 503) {
      const result = classification(response);
      // Graph did not process the POST, so it does not count against the cap.
      await this.fence.recordCanResend().catch(() => undefined);
      return newMutationRetry(failure(op, result.kind, result.message), result.delay);
    }
    if (response.status >= 200 && response.status < 300) {
      let created: Event;
      try {
        created = event(decodeJSON(response.body), this.input.calendarId ?? "", this.display);
        if (created.transactionId && created.transactionId !== this.key) throw new TypeError("another transactionId");
      } catch {
        return this.acceptedWithoutEvent(response);
      }
      await this.recordProviderId(created.id);
      return this.created({ value: created, duplicates: [], response }, false);
    }
    const result = classification(response);
    const problem = failure(op, result.kind, result.message);
    if (response.status === 400 || response.status === 409) {
      const again = await this.lookup();
      if (again.outcome === "found") {
        await this.recordProviderId(again.value.id);
        return this.created(again, true);
      }
      if (again.outcome !== "missing") return this.resendOrUncertain(problem, result.delay, response);
    }
    // Only an allowlisted refusal proves nothing was created; any other answer is unknown.
    if (!isDefiniteGraphRefusal(response)) return this.resendOrUncertain(problem, result.delay, response);
    if (this.state.hasPossibleEarlierSend) return this.uncertain(problem, response);
    // A lost checkpoint leaves the claim, which agent hosts conservatively report as unknown.
    await this.fence.recordCanResend().catch(() => undefined);
    return newMutationBranch("providerRejected", emptyCreate(), receipt(this.call, response, "", this.http.now), problem);
  }

  /** Never resend after a 2xx: record acceptance and read the marker back. */
  private async acceptedWithoutEvent(response: GraphResponse): Promise<MutationAttempt<CreateEventOutput>> {
    const problem = failure("createEvent", "PROTOCOL", "Graph accepted the event, but its response is unusable");
    if (!(await this.recordProviderId(acceptedEventProviderId))) return this.uncertain(problem, response);
    const result = await this.lookup();
    if (result.outcome === "found") {
      await this.recordProviderId(result.value.id);
      return this.created(result, false);
    }
    // The marker index may lag the create; the next attempt only reads back.
    return newMutationRetry(problem);
  }

  private resendOrUncertain(problem: Failure, delay: number, response?: GraphResponse): MutationAttempt<CreateEventOutput> {
    return this.state.canResendUnknownOutcome ? newMutationRetry(problem, delay) : this.uncertain(problem, response);
  }

  private uncertain(problem: Failure, response?: GraphResponse): MutationAttempt<CreateEventOutput> {
    return newMutationUncertain(emptyCreate(), problem, response ? receipt(this.call, response, "", this.http.now) : zeroReceipt());
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
      // A later attempt may follow an applied PATCH, so defect would misreport it.
      if (error instanceof RequestFailure && error.kind === "AUTHENTICATION" && (call.context?.attempt ?? 1) > 1)
        return newMutationRetry(failure(op, error.kind, error.message));
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
    const isLaterAttempt = (call.context?.attempt ?? 1) > 1;
    if (current.outcome === "notFound")
      return newMutationBranch("notFound", empty, readReceipt, current.problem);
    if (current.outcome === "providerRejected" && isDefiniteGraphRefusal(current.response))
      return newMutationBranch("providerRejected", empty, readReceipt, current.problem);
    if (current.outcome === "defect" && !isLaterAttempt)
      return newMutationBranch("defect", empty, zeroReceipt(), current.problem);
    // A transient, unlisted, or unusable pre-write read is unknown, and the update is safe to repeat.
    if (current.outcome !== "found")
      return newMutationRetry(
        current.problem ??
          failure(op, "AVAILABILITY", "provider is unavailable"),
        current.delay,
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
      if (!(error instanceof RequestFailure)) throw error;
      if (error.kind === "LOCAL_DEFECT")
        return newMutationBranch("defect", empty, zeroReceipt(), failure(op, error.kind, error.message));
      return newMutationRetry(failure(op, "TRANSPORT", "update outcome is unknown; the retry rereads the event first"));
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
    if (response.status < 200 || response.status >= 300) {
      const result = classification(response);
      if (result.retry || !isDefiniteGraphRefusal(response))
        return newMutationRetry(failure(op, result.kind, result.message), result.delay);
      return classifiedMutation(op, response, empty, call, this.http);
    }
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
      // The PATCH is absolute, so the retry rereads the event and finds the change if it applied.
      return newMutationRetry(
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
