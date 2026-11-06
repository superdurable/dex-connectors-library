// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  resolveCredential,
  resolveCredentialAfterRejection,
  type Call,
  type CredentialRefreshDriver,
  type Failure,
  type ReceiptDraft,
} from "@superdurable/connectors-sdk";
import {
  BodyTooLargeError,
  isHeaderSafeCredential,
  parseRetryAfter,
  readErrorTokens,
  sendProviderRequest,
  type ProviderRequest,
  type ProviderResponse,
} from "@superdurable/connectors-sdk/providerhttp";
import type {
  Config,
  CredentialSource,
  Credentials,
} from "./generated/models.js";

export const graphBase = "https://graph.microsoft.com/v1.0";
export const marker =
  "String {e8548d82-fea6-423a-bad8-7b6da468e968} Name DexIdempotencyKey";
export const eventSelect =
  "id,subject,body,bodyPreview,start,end,originalStartTimeZone,originalEndTimeZone,isAllDay,isCancelled,isOrganizer,showAs,sensitivity,type,seriesMasterId,location,attendees,organizer,isOnlineMeeting,onlineMeetingProvider,onlineMeeting,webLink,iCalUId,changeKey,transactionId,responseStatus";
export const listSelect = eventSelect.replace("body,", "");
export type Transport = (request: ProviderRequest) => Promise<ProviderResponse>;
export interface GraphResponse {
  status: number;
  headers: ProviderResponse["headers"];
  body: Uint8Array;
}
export class RequestFailure extends Error {
  constructor(
    readonly kind: Failure["kind"],
    message: string,
    readonly response: GraphResponse = {
      status: 0,
      headers: {},
      body: new Uint8Array(),
    },
  ) {
    super(message);
  }
}
export function failure(
  operation: string,
  kind: Failure["kind"],
  message: string,
): Failure {
  return { provider: "outlook-calendar", operation, kind, message };
}
export function receipt(
  call: Call,
  response: GraphResponse,
  objectId = "",
  now: () => Date = () => new Date(),
): ReceiptDraft {
  const candidate = header(response.headers, "request-id");
  return {
    callId: call.id,
    provider: "outlook-calendar",
    providerObjectId: objectId,
    providerRequestId: /^[A-Za-z0-9-]{1,64}$/.test(candidate) ? candidate : "",
    observedAt: now().toISOString(),
    ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
  };
}
export function header(
  headers: ProviderResponse["headers"],
  name: string,
): string {
  const value = headers[name] ?? headers[name.toLowerCase()];
  return (Array.isArray(value) ? value[0] : value) ?? "";
}
export function classification(response: GraphResponse): {
  retry: boolean;
  kind: Failure["kind"];
  message: string;
  delay: number;
} {
  const tokens = readErrorTokens(response.body, ["/error/code"]);
  const code =
    tokens[0] && /^[A-Za-z][A-Za-z0-9]{0,63}$/.test(tokens[0]) ? tokens[0] : "";
  const suffix = code ? ` (${code})` : "";
  const delay = parseRetryAfter(header(response.headers, "retry-after"));
  if (response.status === 429)
    return {
      retry: true,
      kind: "RATE_LIMIT",
      message: "provider throttled the request" + suffix,
      delay,
    };
  if ([408, 500, 502, 503, 504].includes(response.status))
    return {
      retry: true,
      kind: "AVAILABILITY",
      message: "provider is temporarily unavailable" + suffix,
      delay,
    };
  const kinds: Record<number, Failure["kind"]> = {
    400: "VALIDATION",
    422: "VALIDATION",
    401: "AUTHENTICATION",
    403: "AUTHORIZATION",
    404: "NOT_FOUND",
    410: "NOT_FOUND",
    409: "CONFLICT",
    412: "CONFLICT",
  };
  return {
    retry: false,
    kind: ["quotaLimitReached", "ErrorQuotaExceeded"].includes(code)
      ? "QUOTA_EXHAUSTED"
      : (kinds[response.status] ?? "PROVIDER_REJECTION"),
    message:
      `provider rejected the request with HTTP ${response.status}` + suffix,
    delay: 0,
  };
}
function encodeComponent(value: string): string {
  return encodeURIComponent(value).replace(
    /[!'()*]/g,
    (character) => `%${character.charCodeAt(0).toString(16).toUpperCase()}`,
  );
}
/** Go url.Values.Encode order and escaping, followed by Graph's %20 space convention. */
export function graphQuery(values: Record<string, string>): string {
  return Object.keys(values)
    .sort((left, right) =>
      Buffer.compare(Buffer.from(left), Buffer.from(right)),
    )
    .map((key) => `${encodeComponent(key)}=${encodeComponent(values[key]!)}`)
    .join("&");
}
/** Go url.PathEscape safe set. */
export function pathEscape(value: string): string {
  return encodeURIComponent(value)
    .replace(
      /[!'()*]/g,
      (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    )
    .replace(/%24/gi, "$")
    .replace(/%26/gi, "&")
    .replace(/%2B/gi, "+")
    .replace(/%3D/gi, "=")
    .replace(/%3A/gi, ":")
    .replace(/%40/gi, "@");
}
export function calendarPath(root: string, calendarId: string): string {
  return (
    root + (calendarId ? "/calendars/" + pathEscape(calendarId) : "/calendar")
  );
}
export function validateNextLink(value: string): void {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new TypeError(
      "pageToken must be the nextPageToken of a previous page, an https://graph.microsoft.com URL",
    );
  }
  const raw = /^https:\/\/([^/?#]+)([^?#]*)/.exec(value);
  let goPath = "";
  try { goPath = decodeURIComponent(raw?.[2] ?? ""); } catch { /* Invalid percent encoding. */ }
  if (
    !raw || raw[1] !== "graph.microsoft.com" ||
    url.protocol !== "https:" ||
    url.hash ||
    !goPath.startsWith("/v1.0/") ||
    !goPath.toLowerCase().endsWith("/calendarview")
  )
    throw new TypeError(
      "pageToken must be the nextPageToken of a previous page, an https://graph.microsoft.com URL",
    );
}
export function decodeJSON(body: Uint8Array): unknown {
  const text = new TextDecoder("utf-8", { fatal: true }).decode(body);
  return JSON.parse(text, (_key, value: unknown) => {
    if (typeof value === "number" && !Number.isFinite(value))
      throw new TypeError("non-finite JSON number");
    return value;
  }) as unknown;
}
export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new TypeError("provider returned a non-object");
  return value as Record<string, unknown>;
}
export function string(value: unknown): string {
  return typeof value === "string" ? value : "";
}
export class GraphHTTP {
  readonly maxResponseBytes: number;
  readonly mailbox: string;
  readonly baseURL: string;
  constructor(
    config: Config,
    private readonly credentials: CredentialSource,
    private readonly driver: CredentialRefreshDriver<Credentials>,
    private readonly transport: Transport = sendProviderRequest,
    baseURL = graphBase,
    readonly now: () => Date = () => new Date(),
  ) {
    if (!credentials || !driver || !transport)
      throw new TypeError("Outlook Calendar dependencies are required");
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (
      !Number.isSafeInteger(this.maxResponseBytes) ||
      this.maxResponseBytes < 1
    )
      throw new TypeError("Outlook Calendar response limit must be positive");
    const tenant = (config.tenantId ?? "").trim();
    if (
      tenant &&
      !(
        /^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$/.test(tenant) ||
        /^(?:(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)\.)+[A-Za-z]{2,63}$/.test(
          tenant,
        )
      )
    )
      throw new TypeError(
        "tenantId must be a directory (tenant) ID GUID or a verified domain such as contoso.onmicrosoft.com",
      );
    this.mailbox = (config.mailbox ?? "").trim();
    if (
      this.mailbox &&
      !(
        /^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$/.test(
          this.mailbox,
        ) || /^[^\s<>@,;]+@[^\s<>@,;]+$/.test(this.mailbox)
      )
    )
      throw new TypeError(
        "mailbox must be a bare address such as scheduling@contoso.com or a user object ID GUID",
      );
    this.baseURL = baseURL;
  }
  async resolve(
    call: Call,
  ): Promise<{ credentials: Credentials; root: string }> {
    let material: Credentials;
    try {
      material = await resolveCredential(
        this.credentials,
        call,
        this.driver,
        call.context.cancellationSignal,
      );
      const token = material.accessToken?.reveal() ?? "";
      if (
        !isHeaderSafeCredential(token) ||
        !["microsoft-oauth", "app-only"].includes(material.authMethodId ?? "")
      )
        throw new TypeError();
    } catch {
      throw new RequestFailure(
        "AUTHENTICATION",
        "connection credentials are unavailable",
      );
    }
    if (material.authMethodId === "microsoft-oauth")
      return { credentials: material, root: "/me" };
    if (!this.mailbox)
      throw new RequestFailure(
        "VALIDATION",
        "an app-only connection needs its mailbox configured",
      );
    return {
      credentials: material,
      root: "/users/" + pathEscape(this.mailbox),
    };
  }
  async send(
    call: Call,
    credentials: Credentials,
    method: string,
    path: string,
    query?: Record<string, string>,
    payload?: unknown,
    ifMatch = "",
    nextLink = "",
  ): Promise<GraphResponse> {
    const target = nextLink
      ? this.baseURL === graphBase
        ? nextLink
        : nextLink.replace(graphBase, this.baseURL)
      : this.baseURL + path + (query ? "?" + graphQuery(query) : "");
    let body: Uint8Array | undefined;
    try {
      if (payload !== undefined) body = Buffer.from(JSON.stringify(payload));
    } catch {
      throw new RequestFailure(
        "LOCAL_DEFECT",
        "provider request could not be encoded",
      );
    }
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = credentials.accessToken?.reveal() ?? "";
      const request: ProviderRequest = {
        url: target,
        method,
        headers: {
          Authorization: "Bearer " + token,
          Accept: "application/json",
          Prefer: [
            'outlook.timezone="UTC"',
            'outlook.body-content-type="text"',
          ],
          "client-request-id": String(call.id),
          ...(body ? { "Content-Type": "application/json" } : {}),
          ...(ifMatch ? { "If-Match": ifMatch } : {}),
        } as unknown as Record<string, string>,
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.baseURL.startsWith("http://")
          ? "privateNetworkHTTP"
          : "publicHTTPS",
        timeoutMs: 20_000,
        signal: call.context.cancellationSignal,
        ...(body ? { body } : {}),
      };
      let response: GraphResponse;
      let observed:
        | { status: number; headers: ProviderResponse["headers"] }
        | undefined;
      request.onResponseChunk = (_chunk, status, headers) => {
        observed = { status, headers };
      };
      try {
        response = await this.transport(request);
      } catch (error) {
        if (error instanceof BodyTooLargeError)
          throw new RequestFailure(
            "RESPONSE_TOO_LARGE",
            "provider response exceeds configured limit",
            {
              status: error.status ?? observed?.status ?? 0,
              headers: error.headers ?? observed?.headers ?? {},
              body: new Uint8Array(),
            },
          );
        throw new RequestFailure("TRANSPORT", "provider request failed", {
          status: observed?.status ?? 0,
          headers: observed?.headers ?? {},
          body: new Uint8Array(),
        });
      }
      if (
        response.status !== 401 ||
        attempt !== 0 ||
        !("resolveAfterRejection" in this.credentials)
      )
        return response;
      try {
        credentials = await resolveCredentialAfterRejection(
          this.credentials,
          call,
          this.driver,
          call.context.cancellationSignal,
        );
      } catch {
        return response;
      }
      if (!isHeaderSafeCredential(credentials.accessToken?.reveal() ?? "") || !["microsoft-oauth", "app-only"].includes(credentials.authMethodId ?? ""))
        return response;
    }
    throw new RequestFailure(
      "LOCAL_DEFECT",
      "authenticated request retry was exhausted",
    );
  }
}
