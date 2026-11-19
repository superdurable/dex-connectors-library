// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch,
  newMutationRetry,
  newMutationUncertain,
  newQueryBranch,
  newQueryRetry,
  zeroReceipt,
  type Call,
  type Failure,
  type Mutation,
  type MutationAttempt,
  type Query,
  type QueryAttempt,
} from "@superdurable/connectors-sdk";
import {
  SearchConversationsDefinition,
  GetConversationDefinition,
  ReplyToConversationDefinition,
  UpdateConversationStateDefinition,
  FindContactByEmailDefinition,
} from "./generated/definitions.js";
import type {
  SearchConversationsInput,
  SearchConversationsOutput,
  GetConversationInput,
  ConversationDetails,
  ReplyToConversationInput,
  ReplyToConversationOutput,
  UpdateConversationStateInput,
  UpdateConversationStateOutput,
  FindContactByEmailInput,
  FindContactByEmailOutput,
  Conversation,
  ConversationPart,
  Contact,
} from "./generated/models.js";
import type { IntercomClient, IntercomExchange } from "./client.js";
import emailAddresses from "email-addresses";
import { decode } from "html-entities";
import { isIP } from "node:net";

type Wire = Record<string, unknown>;
const zeroTime = "0001-01-01T00:00:00Z";
const numericID = /^[0-9]{1,24}$/;
const contactID = /^[A-Za-z0-9_-]{1,64}$/;
function obj(value: unknown): Wire {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("object is invalid");
  return value as Wire;
}
function str(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value !== "string") throw new Error("string field is invalid");
  return value;
}
function id(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" && Number.isSafeInteger(value)) return String(value);
  throw new Error("identifier is not a string or an integer");
}
function parseProviderJSON(body: Uint8Array): unknown {
  return JSON.parse(
    Buffer.from(body).toString("utf8"),
    (key: string, value: unknown, context?: { source: string }) => {
      if (typeof value !== "number" || !["id", "admin_assignee_id", "team_assignee_id"].includes(key))
        return value;
      const source = context?.source;
      if (source && !/^-?[0-9]{1,24}$/.test(source))
        throw new Error("identifier is not a string or an integer");
      if (!source && !Number.isSafeInteger(value))
        throw new Error("identifier has no exact numeric source");
      return source ?? String(value);
    },
  );
}
function trimGoSpace(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}
function sec(value: unknown): number {
  const text = id(value);
  const n = text ? Number(text) : 0;
  if ((text && !/^-?[0-9]{1,24}$/.test(text)) || !Number.isSafeInteger(n) || n < 0)
    throw new Error("timestamp is not Unix seconds");
  return n;
}
function bool(value: unknown): boolean {
  if (value === null || value === undefined) return false;
  if (typeof value !== "boolean") throw new Error("boolean field is invalid");
  return value;
}
function integerField(value: unknown): number {
  if (value === null || value === undefined) return 0;
  if (typeof value !== "number" || !Number.isSafeInteger(value)) throw new Error("integer field is invalid");
  return value;
}
function time(value: unknown): string {
  const n = sec(value);
  return n ? new Date(n * 1000).toISOString().replace(".000Z", "Z") : zeroTime;
}
function author(value: unknown) {
  const w = obj(value ?? {});
  return {
    ...(str(w.type) ? { type: str(w.type) } : {}),
    ...(id(w.id) ? { id: id(w.id) } : {}),
    ...(str(w.name) ? { name: str(w.name) } : {}),
    ...(str(w.email) ? { email: str(w.email) } : {}),
  };
}
function bodyText(value: unknown): { value: string; isTruncated: boolean } {
  const valueString = str(value);
  const bytes = Buffer.from(valueString);
  if (bytes.length <= 16384) return { value: valueString, isTruncated: false };
  let boundary = 16384;
  while (boundary > 0 && (bytes[boundary]! & 0xc0) === 0x80) boundary--;
  return {
    value: bytes.subarray(0, boundary).toString("utf8"),
    isTruncated: true,
  };
}
function conversation(w: Wire, includeBody: boolean): Conversation {
  if (str(w.type) && str(w.type) !== "conversation")
    throw new Error("object is not a conversation");
  const conversationID = id(w.id);
  if (!numericID.test(conversationID))
    throw new Error("conversation ID is missing or not numeric");
  const source = obj(w.source ?? {});
  const output: Conversation = {
    id: conversationID,
    state: str(w.state),
    isOpen: bool(w.open),
    source: {
      ...(str(source.type) ? { type: str(source.type) } : {}),
      ...(str(source.delivered_as)
        ? { deliveredAs: str(source.delivered_as) }
        : {}),
      ...(str(source.subject) ? { subject: str(source.subject) } : {}),
      ...(includeBody && str(source.body)
        ? {
            body: bodyText(source.body).value,
            ...(bodyText(source.body).isTruncated
              ? { isBodyTruncated: true }
              : {}),
          }
        : {}),
      author: author(source.author),
    },
    createdAt: time(w.created_at),
    updatedAt: time(w.updated_at),
  };
  if (str(w.title)) output.title = str(w.title);
  if (bool(w.read)) output.isRead = true;
  if (str(w.priority)) output.priority = str(w.priority);
  if (id(w.admin_assignee_id) && id(w.admin_assignee_id) !== "0")
    output.adminAssigneeId = id(w.admin_assignee_id);
  if (id(w.team_assignee_id) && id(w.team_assignee_id) !== "0")
    output.teamAssigneeId = id(w.team_assignee_id);
  if (sec(w.waiting_since)) output.waitingSince = time(w.waiting_since);
  if (sec(w.snoozed_until)) output.snoozedUntil = time(w.snoozed_until);
  const contacts = obj(w.contacts ?? {}).contacts;
  if (Array.isArray(contacts) && contacts.length)
    output.contacts = contacts.map((v) => {
      const c = obj(v);
      const contact = id(c.id);
      if (!contact) throw new Error("conversation contact ID is missing");
      return {
        id: contact,
        ...(str(c.external_id) ? { externalId: str(c.external_id) } : {}),
      };
    });
  const tags = obj(w.tags ?? {}).tags;
  if (Array.isArray(tags) && tags.length)
    output.tags = tags.map((v) => {
      const t = obj(v);
      const tag = id(t.id);
      if (!tag) throw new Error("conversation tag ID is missing");
      return { id: tag, ...(str(t.name) ? { name: str(t.name) } : {}) };
    });
  return output;
}
function part(value: unknown): ConversationPart {
  const w = obj(value);
  const partID = id(w.id);
  if (!partID) throw new Error("conversation part ID is missing");
  return {
    id: partID,
    partType: str(w.part_type),
    ...(str(w.body)
      ? {
          body: bodyText(w.body).value,
          ...(bodyText(w.body).isTruncated ? { isBodyTruncated: true } : {}),
        }
      : {}),
    author: author(w.author),
    createdAt: time(w.created_at),
  };
}
function envelope(exchange: IntercomExchange, conversationID: string): Wire {
  if (exchange.outcome !== "succeeded")
    throw new Error("conversation response is unavailable");
  let w: Wire;
  try {
    w = obj(
      parseProviderJSON(successfulResponse(exchange).body),
    );
  } catch {
    throw new Error("conversation response is not a conversation object");
  }
  if (id(w.id) !== conversationID)
    throw new Error("conversation response is for another conversation");
  return w;
}
function parts(w: Wire): Wire[] {
  const list = obj(w.conversation_parts ?? {}).conversation_parts;
  return Array.isArray(list) ? list.map(obj) : [];
}
function zeroConversation(): Conversation {
  return {
    id: "",
    state: "",
    isOpen: false,
    source: { author: {} },
    createdAt: zeroTime,
    updatedAt: zeroTime,
  };
}
function zeroPart(): ConversationPart {
  return { id: "", partType: "", author: {}, createdAt: zeroTime };
}
function zeroDetails(): ConversationDetails {
  return { conversation: zeroConversation(), latestParts: [], partCount: 0 };
}
function zeroSearch(): SearchConversationsOutput {
  return { conversations: [], totalCount: 0 };
}
function zeroReply(conversationID: string): ReplyToConversationOutput {
  return { conversationId: conversationID, part: zeroPart() };
}
function zeroUpdate(): UpdateConversationStateOutput {
  return { conversation: zeroConversation() };
}
function zeroContacts(): FindContactByEmailOutput {
  return { contacts: [] };
}
function failure(
  client: IntercomClient,
  operation: string,
  message: string,
): Failure {
  return client.failure(operation, "VALIDATION", message);
}
function retryable(exchange: IntercomExchange): boolean {
  return (
    exchange.outcome === "notSent" ||
    exchange.outcome === "rateLimited" ||
    exchange.outcome === "unconfirmed"
  );
}
function queryFailure<T>(
  exchange: IntercomExchange,
  empty: T,
  hasNotFound: boolean,
): QueryAttempt<T> | undefined {
  if (exchange.outcome === "succeeded") return;
  if (retryable(exchange))
    return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  const branch =
    exchange.outcome === "notFound" && hasNotFound
      ? "notFound"
      : exchange.outcome === "invalid"
        ? "invalidResponse"
        : exchange.outcome === "defect"
          ? "defect"
          : "providerRejected";
  return newQueryBranch(branch, empty, exchange.receipt, exchange.failure);
}
function mutationFailure<T>(
  exchange: IntercomExchange,
  empty: T,
): MutationAttempt<T> | undefined {
  if (exchange.outcome === "succeeded") return;
  if (retryable(exchange))
    return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  const branch =
    exchange.outcome === "notFound"
      ? "notFound"
      : exchange.outcome === "invalid"
        ? "invalidResponse"
        : exchange.outcome === "defect"
          ? "defect"
          : "providerRejected";
  return newMutationBranch(branch, empty, exchange.receipt, exchange.failure);
}
function rfc3339(value: string): number | undefined {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,]\d+)?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!parts) return;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, , sign, offsetHourText, offsetMinuteText] = parts;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const offsetHour = Number(offsetHourText ?? 0);
  const offsetMinute = Number(offsetMinuteText ?? 0);
  if (month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 || offsetHour > 24 || offsetMinute > 60) return;
  const instant = new Date(0);
  instant.setUTCFullYear(year, month - 1, day);
  instant.setUTCHours(hour, minute, second, 0);
  if (instant.getUTCFullYear() !== year || instant.getUTCMonth() !== month - 1 || instant.getUTCDate() !== day) return;
  const offsetSeconds = (offsetHour * 60 + offsetMinute) * 60;
  return Math.floor(instant.getTime() / 1000) - (sign === "+" ? offsetSeconds : sign === "-" ? -offsetSeconds : 0);
}
function bareEmail(value: string): boolean {
  if (!value || /[ "'()<>,;]/.test(value)) return false;
  const parsed = emailAddresses.parseOneAddress({
    input: value,
    rfc6532: true,
  });
  if (parsed?.type === "mailbox" && parsed.domain?.startsWith("[")) {
    const literal = parsed.domain.slice(1, -1);
    if (isIP(literal) !== 4)
      return false;
  }
  return (
    parsed?.type === "mailbox" &&
    parsed.name === null &&
    parsed.address === value
  );
}
function filter(field: string, value: string): Wire {
  return { field, operator: "=", value };
}
function searchQuery(input: SearchConversationsInput): Wire | string {
  const filters: Wire[] = [];
  const groups: [string, string, string[]][] = [
    ["states", "state", input.states ?? []],
    ["contactIds", "contact_ids", input.contactIds ?? []],
    ["tagIds", "tag_ids", input.tagIds ?? []],
  ];
  for (const [name, field, values] of groups) {
    if (values.length > 15) return `${name} has more than 15 values`;
    if (new Set(values).size !== values.length)
      return `${name} lists an entry twice`;
    if (
      name === "states" &&
      values.some((v) => !["open", "closed", "snoozed"].includes(v))
    )
      return "state is not an Intercom conversation state";
    if (name === "contactIds" && values.some((v) => !contactID.test(v)))
      return "contactIds entry is not an Intercom contact ID";
    if (name === "tagIds" && values.some((v) => !numericID.test(v)))
      return "tagIds entry is not an Intercom tag ID of digits";
    if (values.length) {
      const items = values.map((v) => filter(field, v));
      filters.push(
        items.length === 1 ? items[0]! : { operator: "OR", value: items },
      );
    }
    if (name === "states" && input.contactEmail) {
      if (!bareEmail(input.contactEmail))
        return "contactEmail must be one bare email address";
      filters.push(filter("source.author.email", input.contactEmail));
    }
  }
  if (input.updatedSince) {
    const n = rfc3339(input.updatedSince);
    if (n === undefined || n < 0)
      return "updatedSince must be an RFC 3339 instant";
    filters.push({ field: "updated_at", operator: ">", value: n });
  }
  if (!filters.length)
    return "at least one of states, contactEmail, contactIds, tagIds, or updatedSince is required";
  return filters.length === 1
    ? filters[0]!
    : { operator: "AND", value: filters };
}
function successfulResponse(exchange: IntercomExchange) {
  if (exchange.outcome !== "succeeded")
    throw new Error("provider response is unavailable");
  return exchange.response;
}
function errorText(error: unknown): string {
  return error instanceof Error ? error.message : "invalid data";
}

/** Search one bounded page of Intercom conversations. */
export class SearchConversationsOperation
  implements Query<SearchConversationsInput, SearchConversationsOutput>
{
  /** Bind one client. */ constructor(
    private readonly client: IntercomClient,
  ) {}
  /** Return the manifest definition. */ definition() {
    return SearchConversationsDefinition;
  }
  /** Invoke one search attempt. */ async invoke(
    call: Call,
    input: SearchConversationsInput,
  ): Promise<QueryAttempt<SearchConversationsOutput>> {
    const operation = "searchConversations",
      query = searchQuery(input);
    const invalid =
      typeof query === "string"
        ? query
        : (input.pageSize ?? 0) < 0 || (input.pageSize ?? 0) > 150
          ? "pageSize must be between 1 and 150, or zero for 20"
          : Buffer.byteLength(input.cursor ?? "") > 2048 ||
              /[^!-~]/.test(input.cursor ?? "")
            ? "cursor must be the printable NextCursor of a previous page"
            : undefined;
    if (invalid)
      return newQueryBranch(
        "defect",
        zeroSearch(),
        zeroReceipt(),
        failure(this.client, operation, invalid),
      );
    const exchange = await this.client.exchange(
      call,
      operation,
      "POST",
      "/conversations/search",
      undefined,
      {
        query,
        pagination: {
          per_page: input.pageSize || 20,
          ...(input.cursor ? { starting_after: input.cursor } : {}),
        },
      },
    );
    const bad = queryFailure(exchange, zeroSearch(), false);
    if (bad) return bad;
    try {
      const page = obj(
        parseProviderJSON(successfulResponse(exchange).body),
      );
      if (str(page.type) && str(page.type) !== "conversation.list")
        throw new Error("search page is not a conversation list");
      if (!Array.isArray(page.conversations))
        throw new Error("search page has no conversations array");
      if (page.conversations.length > (input.pageSize || 20))
        throw new Error("search page is larger than requested");
      const output: SearchConversationsOutput = {
        conversations: page.conversations.map((v) =>
          conversation(obj(v), false),
        ),
        totalCount: integerField(page.total_count),
      };
      const next = obj(page.pages ?? {}).next;
      if (next) {
        const cursor = str(obj(next).starting_after);
        if (cursor) {
          if (Buffer.byteLength(cursor) > 2048 || /[^!-~]/.test(cursor))
            throw new Error("search next cursor is invalid");
          output.nextCursor = cursor;
        }
      }
      return newQueryBranch("searched", output, exchange.receipt);
    } catch (error) {
      return newQueryBranch(
        "invalidResponse",
        zeroSearch(),
        exchange.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom returned an invalid search page: ${errorText(error)}`,
        ),
      );
    }
  }
}

/** Read one conversation with its latest parts. */
export class GetConversationOperation
  implements Query<GetConversationInput, ConversationDetails>
{
  /** Bind one client. */ constructor(
    private readonly client: IntercomClient,
  ) {}
  /** Return the manifest definition. */ definition() {
    return GetConversationDefinition;
  }
  /** Invoke one read attempt. */ async invoke(
    call: Call,
    input: GetConversationInput,
  ): Promise<QueryAttempt<ConversationDetails>> {
    const operation = "getConversation";
    if (!numericID.test(input.conversationId))
      return newQueryBranch(
        "defect",
        zeroDetails(),
        zeroReceipt(),
        failure(
          this.client,
          operation,
          "conversationId must be an Intercom conversation ID of digits, such as 215472658213",
        ),
      );
    if ((input.latestPartLimit ?? 0) < 0 || (input.latestPartLimit ?? 0) > 50)
      return newQueryBranch(
        "defect",
        zeroDetails(),
        zeroReceipt(),
        failure(
          this.client,
          operation,
          "latestPartLimit must be between 1 and 50, or zero for 10",
        ),
      );
    const exchange = await this.client.readConversation(
      call,
      operation,
      input.conversationId,
    );
    const bad = queryFailure(exchange, zeroDetails(), true);
    if (bad) return bad;
    try {
      const wire = envelope(exchange, input.conversationId);
      const decoded = parts(wire)
        .map(part)
        .sort(
          (a, b) =>
            (b.createdAt > a.createdAt ? 1 : b.createdAt < a.createdAt ? -1 : 0) || comparePartIDs(b.id, a.id),
        );
      const latestParts = decoded.slice(0, input.latestPartLimit || 10);
      const count = Math.max(
        integerField(obj(wire.conversation_parts ?? {}).total_count),
        decoded.length,
      );
      const output: ConversationDetails = {
        conversation: conversation(wire, true),
        latestParts,
        partCount: count,
        ...(count > latestParts.length ? { hasOlderParts: true } : {}),
      };
      return newQueryBranch("found", output, exchange.receipt);
    } catch (error) {
      return newQueryBranch(
        "invalidResponse",
        zeroDetails(),
        exchange.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom returned an invalid conversation: ${errorText(error)}`,
        ),
      );
    }
  }
}
function comparePartIDs(left: string, right: string): number {
  if (left.length !== right.length && numericID.test(left) && numericID.test(right))
    return left.length > right.length ? 1 : -1;
  return left > right ? 1 : left < right ? -1 : 0;
}

function replyInputError(input: ReplyToConversationInput): string | undefined {
  if (!numericID.test(input.conversationId))
    return "conversationId must be an Intercom conversation ID of digits";
  if (!numericID.test(input.adminId))
    return "adminId must be an Intercom admin ID of digits";
  if (input.messageType !== "comment" && input.messageType !== "note")
    return "messageType must be comment or note";
  if (!trimGoSpace(input.body)) return "body is required";
  if (Buffer.byteLength(input.body) > 16384)
    return "body must be valid UTF-8 of at most 16384 bytes";
  return;
}
function escapeHTML(body: string): string {
  return trimGoSpace(body.replace(/\r\n/g, "\n"))
    .split(/\n[ \t]*\n/)
    .map(trimGoSpace)
    .filter(Boolean)
    .map(
      (p) =>
        `<p>${p.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&#34;").replace(/'/g, "&#39;").replace(/\n/g, "<br>")}</p>`,
    )
    .join("");
}
function plainText(body: string): string {
  return trimGoSpace(decode(body, { level: "html5" }))
    .split(/\p{White_Space}+/u)
    .join(" ");
}
function matchingPart(
  wire: Wire,
  input: ReplyToConversationInput,
  since: number,
  isHTML: boolean,
): Wire | undefined {
  const wanted = plainText(input.body);
  return newestAdminPart(parts(wire).filter(
      (w) =>
        str(w.part_type) === input.messageType &&
        str(obj(w.author ?? {}).type) === "admin" &&
        id(obj(w.author ?? {}).id) === input.adminId &&
        sec(w.created_at) >= since &&
        plainText(
          isHTML ? str(w.body).replace(/<[^>]*>/g, " ") : str(w.body),
        ) === wanted,
    ));
}
function newestAdminPart(candidates: Wire[]): Wire | undefined {
  let newest: Wire | undefined;
  for (const candidate of candidates)
    if (!newest || sec(candidate.created_at) >= sec(newest.created_at)) newest = candidate;
  return newest;
}

/** Add an admin reply at most once per Dex Step execution. */
export class ReplyToConversationOperation
  implements Mutation<ReplyToConversationInput, ReplyToConversationOutput>
{
  /** Bind one client. */ constructor(
    private readonly client: IntercomClient,
  ) {}
  /** Return the manifest definition. */ definition() {
    return ReplyToConversationDefinition;
  }
  /** Correlate the call with its stable ID. */ idempotencyKey(
    callId: string,
  ): string {
    return callId;
  }
  /** Invoke or reconcile one reply attempt. */ async invoke(
    call: Call,
    input: ReplyToConversationInput,
  ): Promise<MutationAttempt<ReplyToConversationOutput>> {
    const operation = "replyToConversation",
      output = zeroReply(input.conversationId),
      error = replyInputError(input);
    if (error)
      return newMutationBranch(
        "defect",
        output,
        zeroReceipt(),
        failure(this.client, operation, error),
      );
    let marker: Wire | undefined;
    try {
      if (call.context.hasLastHeartbeatValue()) {
        const previous = call.context.getLastHeartbeatValue();
        if (previous != null) marker = obj(previous);
      }
    } catch {
      marker = { intercomReplyDispatchedAt: 0 };
    }
    if (marker) return this.reconcile(call, input, marker);
    const dispatchedAt = Math.floor(this.client.now().getTime() / 1000);
    try {
      await call.context.recordHeartbeat({
        intercomReplyDispatchedCallId: call.id,
        intercomReplyDispatchedAt: dispatchedAt,
      });
    } catch {
      return newMutationRetry(
        this.client.failure(
          operation,
          "AVAILABILITY",
          "Dex did not record the dispatch checkpoint; nothing was sent",
        ),
      );
    }
    const exchange = await this.client.exchange(
      call,
      operation,
      "POST",
      `/conversations/${input.conversationId}/reply`,
      undefined,
      {
        message_type: input.messageType,
        type: "admin",
        admin_id: input.adminId,
        body: escapeHTML(input.body),
      },
      input.conversationId,
    );
    if (
      exchange.outcome === "notSent" ||
      exchange.outcome === "rateLimited" ||
      exchange.outcome === "defect"
    ) {
      try {
        await call.context.recordHeartbeat(null);
      } catch {
        /* A retained checkpoint forces safe read-back next attempt. */
      }
    }
    const bad = mutationFailure(exchange, output);
    if (bad) return bad;
    try {
      const wire = envelope(exchange, input.conversationId);
      if (str(wire.state)) output.conversationState = str(wire.state);
      const found =
        matchingPart(wire, input, dispatchedAt - 300, true) ??
        newestAdminPart(parts(wire).filter(
          (w) =>
            str(w.part_type) === input.messageType &&
            str(obj(w.author ?? {}).type) === "admin" &&
            id(obj(w.author ?? {}).id) === input.adminId &&
            sec(w.created_at) >= dispatchedAt - 300,
        ));
      if (found) output.part = part(found);
      return newMutationBranch(
        "replied",
        output,
        output.part.id
          ? { ...exchange.receipt, metadata: { partId: output.part.id } }
          : exchange.receipt,
      );
    } catch (error) {
      return newMutationBranch(
        "invalidResponse",
        output,
        exchange.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom accepted the reply but returned an invalid conversation: ${errorText(error)}`,
        ),
      );
    }
  }
  private async reconcile(
    call: Call,
    input: ReplyToConversationInput,
    marker: Wire,
  ): Promise<MutationAttempt<ReplyToConversationOutput>> {
    const operation = "replyToConversation",
      output = zeroReply(input.conversationId),
      exchange = await this.client.readConversation(
        call,
        operation,
        input.conversationId,
      );
    if (exchange.outcome !== "succeeded" && retryable(exchange))
      return newMutationRetry(exchange.failure, exchange.retryAfterMs);
    if (exchange.outcome === "notFound")
      return newMutationBranch(
        "notFound",
        output,
        exchange.receipt,
        exchange.failure,
      );
    if (exchange.outcome !== "succeeded")
      return newMutationUncertain(
        output,
        this.client.failure(
          operation,
          exchange.failure.kind,
          "an earlier attempt of this Step sent the reply, and the conversation could not be read back",
        ),
        exchange.receipt,
      );
    try {
      const wire = envelope(exchange, input.conversationId);
      if (str(wire.state)) output.conversationState = str(wire.state);
      const found = matchingPart(
        wire,
        input,
        Number(marker.intercomReplyDispatchedAt ?? 0) - 300,
        false,
      );
      if (!found)
        return newMutationUncertain(
          output,
          this.client.failure(
            operation,
            "TRANSPORT",
            "an earlier attempt of this Step sent the reply without a confirmed outcome, and the conversation shows no matching part, so it is not sent again",
          ),
          exchange.receipt,
        );
      output.part = part(found);
      output.wasAlreadyApplied = true;
      return newMutationBranch("replied", output, {
        ...exchange.receipt,
        metadata: { partId: output.part.id },
      });
    } catch (error) {
      return newMutationUncertain(
        output,
        this.client.failure(
          operation,
          "PROTOCOL",
          `an earlier attempt of this Step sent the reply, and the conversation read back is invalid: ${errorText(error)}`,
        ),
        exchange.receipt,
      );
    }
  }
}

function updateInputTime(input: UpdateConversationStateInput): number | string {
  if (!numericID.test(input.conversationId))
    return "conversationId must be an Intercom conversation ID of digits";
  if (!numericID.test(input.adminId))
    return "adminId must be an Intercom admin ID of digits";
  if (!["open", "closed", "snoozed"].includes(input.state))
    return "state is not an Intercom conversation state";
  if (input.state !== "snoozed")
    return input.snoozedUntil
      ? "snoozedUntil is only used with state snoozed"
      : 0;
  const n = rfc3339(input.snoozedUntil ?? "");
  return n && n > 0
    ? n
    : "snoozedUntil must be an RFC 3339 instant with an explicit offset";
}
function stateMatches(
  conversation: Conversation,
  input: UpdateConversationStateInput,
  snoozedUntil: number,
): boolean {
  return (
    conversation.state === input.state &&
    (input.state !== "snoozed" ||
      (conversation.snoozedUntil != null &&
        rfc3339(conversation.snoozedUntil) === snoozedUntil))
  );
}
function stateRequest(
  input: UpdateConversationStateInput,
  snoozedUntil: number,
): Wire {
  return input.state === "closed"
    ? { message_type: "close", type: "admin", admin_id: input.adminId }
    : input.state === "snoozed"
      ? {
          message_type: "snoozed",
          admin_id: input.adminId,
          snoozed_until: snoozedUntil,
        }
      : { message_type: "open", admin_id: input.adminId };
}

/** Change a conversation state after checking its current value. */
export class UpdateConversationStateOperation
  implements
    Mutation<UpdateConversationStateInput, UpdateConversationStateOutput>
{
  /** Bind one client. */ constructor(
    private readonly client: IntercomClient,
  ) {}
  /** Return the manifest definition. */ definition() {
    return UpdateConversationStateDefinition;
  }
  /** Correlate the call with its stable ID. */ idempotencyKey(
    callId: string,
  ): string {
    return callId;
  }
  /** Read, conditionally write, and reconcile a rejection. */ async invoke(
    call: Call,
    input: UpdateConversationStateInput,
  ): Promise<MutationAttempt<UpdateConversationStateOutput>> {
    const operation = "updateConversationState",
      snoozedUntil = updateInputTime(input);
    if (typeof snoozedUntil === "string")
      return newMutationBranch(
        "defect",
        zeroUpdate(),
        zeroReceipt(),
        failure(this.client, operation, snoozedUntil),
      );
    const first = await this.client.readConversation(
        call,
        operation,
        input.conversationId,
      ),
      bad = mutationFailure(first, zeroUpdate());
    if (bad) return bad;
    let current: Conversation;
    try {
      current = conversation(envelope(first, input.conversationId), false);
    } catch (error) {
      return newMutationBranch(
        "invalidResponse",
        zeroUpdate(),
        first.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom returned an invalid conversation: ${errorText(error)}`,
        ),
      );
    }
    if (stateMatches(current, input, snoozedUntil))
      return newMutationBranch(
        "updated",
        { conversation: current, wasAlreadyApplied: true },
        this.client.receipt(call, input.conversationId),
      );
    const write = await this.client.exchange(
      call,
      operation,
      "POST",
      `/conversations/${input.conversationId}/parts`,
      undefined,
      stateRequest(input, snoozedUntil),
      input.conversationId,
    );
    if (write.outcome === "rejected") {
      const reread = await this.client.readConversation(
          call,
          operation,
          input.conversationId,
        ),
        readFailure = mutationFailure(reread, zeroUpdate());
      if (readFailure) return readFailure;
      try {
        const after = conversation(
          envelope(reread, input.conversationId),
          false,
        );
        return stateMatches(after, input, snoozedUntil)
          ? newMutationBranch(
              "updated",
              { conversation: after, wasAlreadyApplied: true },
              write.receipt,
            )
          : newMutationBranch(
              "providerRejected",
              { conversation: after },
              write.receipt,
              write.failure,
            );
      } catch (error) {
        return newMutationBranch(
          "invalidResponse",
          zeroUpdate(),
          reread.receipt,
          this.client.failure(
            operation,
            "PROTOCOL",
            `Intercom returned an invalid conversation: ${errorText(error)}`,
          ),
        );
      }
    }
    const writeFailure = mutationFailure(write, { conversation: current });
    if (writeFailure) return writeFailure;
    try {
      const after = conversation(envelope(write, input.conversationId), false);
      if (!stateMatches(after, input, snoozedUntil))
        throw new Error(
          "conversation returned after the change is not in the requested state",
        );
      return newMutationBranch(
        "updated",
        { conversation: after },
        write.receipt,
      );
    } catch (error) {
      return newMutationBranch(
        "invalidResponse",
        { conversation: current },
        write.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom returned an invalid conversation: ${errorText(error)}`,
        ),
      );
    }
  }
}

/** Find Intercom contacts with one bare email address. */
export class FindContactByEmailOperation
  implements Query<FindContactByEmailInput, FindContactByEmailOutput>
{
  /** Bind one client. */ constructor(
    private readonly client: IntercomClient,
  ) {}
  /** Return the manifest definition. */ definition() {
    return FindContactByEmailDefinition;
  }
  /** Invoke one contact search. */ async invoke(
    call: Call,
    input: FindContactByEmailInput,
  ): Promise<QueryAttempt<FindContactByEmailOutput>> {
    const operation = "findContactByEmail";
    if (!bareEmail(input.email))
      return newQueryBranch(
        "defect",
        zeroContacts(),
        zeroReceipt(),
        failure(
          this.client,
          operation,
          "email must be one bare email address such as jane@example.com",
        ),
      );
    if ((input.contactLimit ?? 0) < 0 || (input.contactLimit ?? 0) > 50)
      return newQueryBranch(
        "defect",
        zeroContacts(),
        zeroReceipt(),
        failure(
          this.client,
          operation,
          "contactLimit must be between 1 and 50, or zero for 10",
        ),
      );
    const limit = input.contactLimit || 10,
      exchange = await this.client.exchange(
        call,
        operation,
        "POST",
        "/contacts/search",
        undefined,
        {
          query: filter("email", input.email),
          pagination: { per_page: limit },
        },
      ),
      bad = queryFailure(exchange, zeroContacts(), false);
    if (bad) return bad;
    try {
      const page = obj(
        parseProviderJSON(successfulResponse(exchange).body),
      );
      if (str(page.type) && str(page.type) !== "list")
        throw new Error("contact page is not a contact list");
      if (!Array.isArray(page.data))
        throw new Error("contact page has no data array");
      if (page.data.length > limit)
        throw new Error("contact page is larger than requested");
      const contacts: Contact[] = page.data.map((v) => {
        const w = obj(v);
        if (str(w.type) && str(w.type) !== "contact")
          throw new Error("contact is not a contact");
        const contact = id(w.id);
        if (!contactID.test(contact))
          throw new Error("contact has no valid ID");
        return {
          id: contact,
          role: str(w.role),
          ...(str(w.email) ? { email: str(w.email) } : {}),
          ...(str(w.name) ? { name: str(w.name) } : {}),
          ...(str(w.external_id) ? { externalId: str(w.external_id) } : {}),
          createdAt: time(w.created_at),
          updatedAt: time(w.updated_at),
        };
      });
      const hasMore =
        (obj(page.pages ?? {}).next != null && str(obj(obj(page.pages ?? {}).next).starting_after) !== "") ||
        integerField(page.total_count) > contacts.length;
      const output: FindContactByEmailOutput = {
        contacts,
        ...(hasMore ? { hasMoreContacts: true } : {}),
      };
      return contacts.length
        ? newQueryBranch("found", output, exchange.receipt)
        : newQueryBranch(
            "notFound",
            output,
            exchange.receipt,
            this.client.failure(
              operation,
              "NOT_FOUND",
              "no Intercom contact has the email address",
            ),
          );
    } catch (error) {
      return newQueryBranch(
        "invalidResponse",
        zeroContacts(),
        exchange.receipt,
        this.client.failure(
          operation,
          "PROTOCOL",
          `Intercom returned an invalid contact page: ${errorText(error)}`,
        ),
      );
    }
  }
}
