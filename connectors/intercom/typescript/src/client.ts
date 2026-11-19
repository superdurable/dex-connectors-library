// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  AddressRefusal,
  BodyTooLargeError,
  isHeaderSafeCredential,
  parseRetryAfter,
  ProviderTransportError,
  sendProviderRequest,
  validateBaseURL,
  type ProviderRequest,
  type ProviderResponse,
} from "@superdurable/connectors-sdk/providerhttp";
import {
  SecretString,
  type Call,
  type CredentialProvider,
  type Failure,
  type ReceiptDraft,
} from "@superdurable/connectors-sdk";
import type { Config, Credentials } from "./generated/models.js";
import {
  SearchConversationsOperation,
  GetConversationOperation,
  ReplyToConversationOperation,
  UpdateConversationStateOperation,
  FindContactByEmailOperation,
} from "./operations.js";

const regionalEndpoints: Record<string, string> = {
  us: "https://api.intercom.io",
  eu: "https://api.eu.intercom.io",
  au: "https://api.au.intercom.io",
};
const apiVersion = "2.16";

/** Provider request sender injectable for safe deterministic tests. */
export type IntercomRequestSender = (
  request: ProviderRequest,
) => Promise<ProviderResponse>;

/** Client dependencies for provider tests. */
export interface IntercomClientOptions {
  /** Override the regional API host with a loopback fake. */
  endpoint?: string;
  /** Override the SDK transport. */
  send?: IntercomRequestSender;
  /** Override receipt and rate limit clock. */
  now?: () => Date;
}

/** One classified provider exchange. */
export type IntercomExchange =
  | { outcome: "succeeded"; response: ProviderResponse; receipt: ReceiptDraft }
  | {
      outcome:
        | "notFound"
        | "notSent"
        | "rateLimited"
        | "unconfirmed"
        | "rejected"
        | "invalid"
        | "defect";
      failure: Failure;
      retryAfterMs: number;
      receipt: ReceiptDraft;
    };

/** Intercom API client with live token resolution and regional routing. */
export class IntercomClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentialProvider: CredentialProvider<Credentials>;
  private readonly sender: IntercomRequestSender;
  readonly now: () => Date;

  /** Construct one validated client without contacting Intercom. */
  constructor(
    config: Config,
    credentialProvider: CredentialProvider<Credentials>,
    options: IntercomClientOptions = {},
  ) {
    if (!credentialProvider)
      throw new TypeError("Intercom credential provider is required");
    const region = config.region || "us";
    if (!(region in regionalEndpoints))
      throw new TypeError("Intercom region must be us, eu, or au");
    this.endpoint = validateBaseURL(
      options.endpoint ?? regionalEndpoints[region]!,
    );
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (
      !Number.isSafeInteger(this.maxResponseBytes) ||
      this.maxResponseBytes <= 0 ||
      (config.webhookMaxBodyBytes !== undefined &&
        (!Number.isSafeInteger(config.webhookMaxBodyBytes) ||
          config.webhookMaxBodyBytes <= 0))
    )
      throw new TypeError(
        "Intercom response and webhook body limits must be positive",
      );
    this.credentialProvider = credentialProvider;
    this.sender = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the conversation search query. */
  searchConversations(): SearchConversationsOperation {
    return new SearchConversationsOperation(this);
  }
  /** Bind the conversation read query. */
  getConversation(): GetConversationOperation {
    return new GetConversationOperation(this);
  }
  /** Bind the reply mutation. */
  replyToConversation(): ReplyToConversationOperation {
    return new ReplyToConversationOperation(this);
  }
  /** Bind the state mutation. */
  updateConversationState(): UpdateConversationStateOperation {
    return new UpdateConversationStateOperation(this);
  }
  /** Bind the contact lookup query. */
  findContactByEmail(): FindContactByEmailOperation {
    return new FindContactByEmailOperation(this);
  }

  /** Create a secret-safe failure with stable operation identity. */
  failure(operation: string, kind: Failure["kind"], message: string): Failure {
    return { provider: "intercom", operation, kind, message };
  }

  /** Create a provider receipt from a response. */
  receipt(call: Call, objectId = "", requestId = ""): ReceiptDraft {
    return {
      callId: call.id,
      provider: "intercom",
      observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestId ? { providerRequestId: requestId } : {}),
    };
  }

  /** Send and classify one authenticated API request. */
  async exchange(
    call: Call,
    operation: string,
    method: string,
    path: string,
    query?: URLSearchParams,
    payload?: unknown,
    objectId = "",
  ): Promise<IntercomExchange> {
    let token: string;
    try {
      const credentials = await this.credentialProvider.resolve(
        call,
        call.context.cancellationSignal,
      );
      if (!(credentials.accessToken instanceof SecretString))
        throw new TypeError("Intercom access token is missing");
      token = credentials.accessToken.reveal();
      if (!isHeaderSafeCredential(token))
        throw new TypeError("Intercom access token is invalid");
    } catch {
      return {
        outcome: "defect",
        failure: this.failure(
          operation,
          "AUTHENTICATION",
          "connection credentials are unavailable",
        ),
        retryAfterMs: 0,
        receipt: this.receipt(call, objectId),
      };
    }
    let body: Uint8Array | undefined;
    try {
      if (payload !== undefined)
        body = Buffer.from(JSON.stringify(payload), "utf8");
    } catch {
      return {
        outcome: "defect",
        failure: this.failure(
          operation,
          "LOCAL_DEFECT",
          "Intercom request could not be built",
        ),
        retryAfterMs: 0,
        receipt: this.receipt(call, objectId),
      };
    }
    const url = this.endpoint + path + (query?.size ? `?${query}` : "");
    const headers = {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
      "Intercom-Version": apiVersion,
      ...(body ? { "Content-Type": "application/json" } : {}),
    };
    let response: ProviderResponse;
    try {
      response = await this.sender({
        url,
        method,
        headers,
        ...(body ? { body } : {}),
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.endpoint.startsWith("https://api.")
          ? "publicHTTPS"
          : "privateNetworkHTTP",
        timeoutMs: 9_000,
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      const receipt = this.receipt(call, objectId);
      if (
        error instanceof BodyTooLargeError &&
        error.status !== undefined &&
        (error.status < 200 || error.status >= 300)
      ) {
        response = {
          status: error.status,
          headers: error.headers ?? {},
          body: new Uint8Array(),
        };
      } else if (error instanceof BodyTooLargeError)
        return {
          outcome: "invalid",
          failure: this.failure(
            operation,
            "RESPONSE_TOO_LARGE",
            "Intercom response exceeds the configured maxResponseBytes limit",
          ),
          retryAfterMs: 0,
          receipt,
        };
      else if (error instanceof AddressRefusal || error instanceof ProviderTransportError && error.isProvablyNotSent)
        return {
          outcome: "notSent",
          failure: this.failure(
            operation,
            "TRANSPORT",
            "Intercom could not be reached; no request was sent",
          ),
          retryAfterMs: 0,
          receipt,
        };
      else
        return {
          outcome: "unconfirmed",
          failure: this.failure(
            operation,
            "TRANSPORT",
            "Intercom request failed before a response arrived",
          ),
          retryAfterMs: 0,
          receipt,
        };
    }
    const receipt = this.receipt(call, objectId);
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(token)))
        return {
          outcome: "invalid",
          failure: this.failure(
            operation,
            "PROTOCOL",
            "Intercom response reflected the connection credential",
          ),
          retryAfterMs: 0,
          receipt,
        };
      return { outcome: "succeeded", response, receipt };
    }
    const status = response.status;
    const errorSummary = describeIntercomError(response.body);
    const failureReceipt = this.receipt(call, objectId, errorSummary.requestId);
    if (status === 429)
      return {
        outcome: "rateLimited",
        failure: this.failure(
          operation,
          "RATE_LIMIT",
          withSummary(
            "Intercom rate limited the request (HTTP 429)",
            errorSummary,
          ),
        ),
        retryAfterMs: rateLimitDelay(response, this.now()),
        receipt: failureReceipt,
      };
    if (status === 408 || status >= 500)
      return {
        outcome: "unconfirmed",
        failure: this.failure(
          operation,
          "AVAILABILITY",
          withSummary(
            `Intercom could not complete the request (HTTP ${status})`,
            errorSummary,
          ),
        ),
        retryAfterMs: 0,
        receipt: failureReceipt,
      };
    if (status === 404 || status === 410)
      return {
        outcome: "notFound",
        failure: this.failure(
          operation,
          "NOT_FOUND",
          withSummary(
            `Intercom found no such resource (HTTP ${status})`,
            errorSummary,
          ),
        ),
        retryAfterMs: 0,
        receipt: failureReceipt,
      };
    if (status >= 300 && status < 400)
      return {
        outcome: "rejected",
        failure: this.failure(
          operation,
          "PROTOCOL",
          `Intercom redirected the request (HTTP ${status}); check the region configuration`,
        ),
        retryAfterMs: 0,
        receipt: failureReceipt,
      };
    if (status === 401)
      return {
        outcome: "rejected",
        failure: this.failure(
          operation,
          "AUTHENTICATION",
          withSummary(
            "Intercom rejected the access token (HTTP 401); check the token and that the region matches the workspace",
            errorSummary,
          ),
        ),
        retryAfterMs: 0,
        receipt: failureReceipt,
      };
    const kind: Failure["kind"] =
      status === 403
        ? "AUTHORIZATION"
        : status === 409
          ? "CONFLICT"
          : status === 400 || status === 422
            ? "VALIDATION"
            : "PROVIDER_REJECTION";
    return {
      outcome: "rejected",
      failure: this.failure(
        operation,
        kind,
        withSummary(
          `Intercom rejected the request (HTTP ${status})`,
          errorSummary,
        ),
      ),
      retryAfterMs: 0,
      receipt: failureReceipt,
    };
  }

  /** Read a conversation with its parts rendered as plain text. */
  readConversation(
    call: Call,
    operation: string,
    conversationId: string,
  ): Promise<IntercomExchange> {
    return this.exchange(
      call,
      operation,
      "GET",
      `/conversations/${conversationId}`,
      new URLSearchParams({ display_as: "plaintext" }),
      undefined,
      conversationId,
    );
  }
}

interface ErrorSummary {
  codes: string[];
  fields: string[];
  requestId: string;
}
function describeIntercomError(body: Uint8Array): ErrorSummary {
  const summary: ErrorSummary = { codes: [], fields: [], requestId: "" };
  let document: unknown;
  try {
    document = JSON.parse(Buffer.from(body).toString("utf8")) as unknown;
  } catch {
    return summary;
  }
  if (!document || typeof document !== "object") return summary;
  const candidate = document as { request_id?: unknown; errors?: unknown };
  if (
    typeof candidate.request_id === "string" &&
    /^[A-Za-z0-9-]{1,64}$/.test(candidate.request_id)
  )
    summary.requestId = candidate.request_id;
  if (Array.isArray(candidate.errors))
    for (const entry of candidate.errors.slice(0, 3)) {
      if (!entry || typeof entry !== "object") continue;
      const error = entry as { code?: unknown; field?: unknown };
      if (
        typeof error.code === "string" &&
        /^[a-z][a-z0-9_.]{0,63}$/.test(error.code)
      )
        summary.codes.push(error.code);
      if (
        typeof error.field === "string" &&
        /^[a-z][a-z0-9_.]{0,63}$/.test(error.field)
      )
        summary.fields.push(error.field);
    }
  return summary;
}
function withSummary(message: string, summary: ErrorSummary): string {
  const parts = [
    ...(summary.codes.length ? [summary.codes.join(", ")] : []),
    ...(summary.fields.length ? [`fields: ${summary.fields.join(", ")}`] : []),
  ];
  return parts.length ? `${message} [${parts.join("; ")}]` : message;
}
function rateLimitDelay(response: ProviderResponse, now: Date): number {
  const retryAfter = response.headers["retry-after"];
  const parsedRetryAfter = parseRetryAfter(
    Array.isArray(retryAfter) ? (retryAfter[0] ?? "") : (retryAfter ?? ""),
    now,
  );
  if (parsedRetryAfter > 0) return Math.min(parsedRetryAfter, 60_000);
  const reset = response.headers["x-ratelimit-reset"];
  const resetSeconds = Number(Array.isArray(reset) ? reset[0] : reset);
  if (!Number.isSafeInteger(resetSeconds) || resetSeconds <= 0) return 0;
  return Math.min(
    60_000,
    Math.max(1_000, resetSeconds * 1_000 - now.getTime()),
  );
}
