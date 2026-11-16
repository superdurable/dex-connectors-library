// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The fake's API version segment; configure the client endpoint with it. */
export const apiVersionPath = '/2010-04-01';

interface FakeMessage { sid: string; from: string; to: string; body: string; status: string; direction: string; sentAtMs: number }

function rfc1123z(milliseconds: number): string {
  const moment = new Date(milliseconds);
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${days[moment.getUTCDay()]}, ${pad(moment.getUTCDate())} ${months[moment.getUTCMonth()]} ${moment.getUTCFullYear()} ${pad(moment.getUTCHours())}:${pad(moment.getUTCMinutes())}:${pad(moment.getUTCSeconds())} +0000`;
}

function jsonResponse(status: number, value: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(value)) };
}

/**
 * A credential-safe stand-in for the Twilio Messages list API, ported from the Go internal/twiliofake: it serves
 * DateSent> and DateSent< (inclusive, to the second), To, PageSize, and next_page_uri with a PageToken, newest
 * first, and accepts only its own Basic credential.
 */
export class TwilioMessagesFake {
  /** The query of every list request served, including throttled ones. */ readonly listRequests: Array<Record<string, string>> = [];
  private readonly expectedAuthorization: string;
  // The nonce keeps SIDs, and so Flow IDs, unique across test runs against one Dex server.
  private readonly nonce = String(process.hrtime.bigint()) + String(Date.now());
  private readonly messages: FakeMessage[] = [];
  private readonly throttles: number[] = [];
  private next = 0;
  private isDateOnly = false;

  /** Serve accountSid with one Basic credential. */
  constructor(readonly accountSid: string, username: string, password: string) {
    this.expectedAuthorization = `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`;
  }

  /** Add an inbound message; status is received unless the caller passes receiving. */
  receive(from: string, to: string, body: string, sentAt: Date, status = ''): string { return this.add(from, to, body, sentAt, status || 'received', 'inbound'); }
  /** Add an outbound-api message, which the Trigger must ignore. */
  sendOutbound(from: string, to: string, body: string, sentAt: Date): string { return this.add(from, to, body, sentAt, 'delivered', 'outbound-api'); }
  /** Change a message's status, as Twilio does when processing finishes. */
  setStatus(messageSid: string, status: string): void { for (const message of this.messages) if (message.sid === messageSid) message.status = status; }
  /** Compare DateSent bounds as whole GMT dates, the precision Twilio's reference documents. */
  useDateOnlyFilters(): void { this.isDateOnly = true; }
  /** Answer the next list request with 429 and Retry-After seconds; zero omits the header. */
  throttleNextList(retryAfterSeconds: number): void { this.throttles.push(retryAfterSeconds); }

  /** The TwilioClient send function. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => this.serve(request);

  private add(from: string, to: string, body: string, sentAt: Date, status: string, direction: string): string {
    this.next++;
    const digest = createHash('sha256').update(`${this.nonce}${this.accountSid}${this.next}${to}${body}`).digest('hex');
    const message = { sid: `SM${digest.slice(0, 32)}`, from, to, body, status, direction, sentAtMs: Math.floor(sentAt.getTime() / 1000) * 1000 };
    this.messages.push(message);
    return message.sid;
  }

  private resource(message: FakeMessage): Record<string, unknown> {
    const dated = rfc1123z(message.sentAtMs);
    return {
      sid: message.sid, account_sid: this.accountSid, from: message.from, to: message.to, body: message.body, status: message.status,
      direction: message.direction, num_segments: '1', num_media: '0', date_created: dated, date_updated: dated, date_sent: dated,
    };
  }

  private serve(request: ProviderRequest): ProviderResponse {
    const listPath = `${apiVersionPath}/Accounts/${this.accountSid}/Messages.json`;
    const messagePrefix = listPath.replace(/\.json$/, '/');
    if (request.headers?.Authorization !== this.expectedAuthorization) return jsonResponse(401, { code: 20003, status: 401 });
    const url = new URL(request.url);
    if (request.method === 'GET' && url.pathname.startsWith(messagePrefix)) {
      const sid = url.pathname.slice(messagePrefix.length).replace(/\.json$/, '');
      const message = this.messages.find(entry => entry.sid === sid);
      return message ? jsonResponse(200, this.resource(message)) : jsonResponse(404, { code: 20404, status: 404 });
    }
    if (request.method !== 'GET' || url.pathname !== listPath) return jsonResponse(404, { code: 20404, status: 404 });
    const query = Object.fromEntries(url.searchParams);
    this.listRequests.push(query);
    const throttle = this.throttles.shift();
    if (throttle !== undefined) return jsonResponse(429, { code: 20429, status: 429 }, throttle > 0 ? { 'retry-after': String(throttle) } : {});
    const bounds: Record<string, number> = {};
    for (const name of ['DateSent>', 'DateSent<']) {
      const value = query[name];
      if (value === undefined) continue;
      let bound = Date.parse(value);
      if (Number.isNaN(bound)) return jsonResponse(400, { code: 20001, status: 400 });
      if (this.isDateOnly) {
        bound -= bound % 86_400_000;
        if (name === 'DateSent<') bound += 86_400_000 - 1000;
      }
      bounds[name] = bound;
    }
    const since = bounds['DateSent>'], until = bounds['DateSent<'];
    const matched = this.messages
      .filter(message => (!query.To || message.to === query.To) && (since === undefined || message.sentAtMs >= since) && (until === undefined || message.sentAtMs <= until))
      .sort((left, right) => right.sentAtMs - left.sentAtMs || (right.sid < left.sid ? -1 : right.sid > left.sid ? 1 : 0));
    const pageSize = Number(query.PageSize ?? '50');
    const offset = Number((query.PageToken ?? 'PA0').slice(2));
    const end = Math.min(offset + pageSize, matched.length);
    let nextPageURI: string | null = null;
    if (end < matched.length) {
      const following = new URLSearchParams({ ...query, PageToken: `PA${end}`, Page: String(Math.floor(end / pageSize)) });
      following.sort();
      nextPageURI = `${listPath}?${following.toString()}`;
    }
    return jsonResponse(200, { messages: matched.slice(offset, end).map(message => this.resource(message)), page_size: pageSize, next_page_uri: nextPageURI });
  }
}
