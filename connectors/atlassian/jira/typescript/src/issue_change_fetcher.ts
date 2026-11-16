// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { JiraSessionFailure, firstHeader, retryAfter, type JiraClient, type JiraSession } from './client.js';
import type { Issue } from './generated/models.js';
import { decodeIssueChangeCursor, encodeIssueChangeCursor, issueChangeLookbackMs, issueVersionKey, newIssueChangeCursor, recentSeenVersions, type IssueChangeCursor } from './issue_change_cursor.js';
import { decodeObject, decodeIssue, parseJiraTime, rejectedFields, rejection } from './operations.js';
import { IssueChangedTriggerDefinition, type IssueChangedEvent, type IssueChangedTriggerConfiguration } from './issue_changed.js';

const operation = 'issueChanged';
const searchPath = '/search/jql';
const searchSubject = 'issue search';
/** Jira caps enhanced search pages with standard fields at 100. */
const pageSize = 100;
const maximumSeedPages = 10;
/** Pages follow one token only this far; Jira does not promise tokens survive reordering. */
const pageChainProgressMs = 60_000;
const maximumPageTokenBytes = 4096;
const createdUpdatedToleranceMs = 1_000;
/** Go's zero time.Time, 0001-01-01T00:00:00Z, which Jira omits created times decode to. */
const zeroTimeMillis = -62_135_596_800_000;
const standardFields = ['summary', 'status', 'issuetype', 'project', 'assignee', 'reporter', 'priority', 'labels', 'created', 'updated'];

interface SearchPage { issues: unknown[]; nextPageToken: string; isPageTokenRejected: boolean }

/** Return the JQL clause value and the cursor identity of the watched project; both are empty for every browsable project. */
function watchedProject(configuration: IssueChangedTriggerConfiguration): [string, string] {
  if (configuration.projectId) return [configuration.projectId, `id:${configuration.projectId}`];
  if (configuration.projectKey) return [`"${configuration.projectKey}"`, `key:${configuration.projectKey}`];
  return ['', ''];
}

/** Return Unix milliseconds of an RFC 3339 UTC instant, truncating sub-millisecond digits as Go's UnixMilli does. */
export function unixMilliseconds(instant: string): number {
  const whole = Date.parse(instant.slice(0, 19) + 'Z');
  const fraction = /\.(\d+)Z$/.exec(instant)?.[1] ?? '';
  return whole + Number((fraction + '000').slice(0, 3));
}

/**
 * Return the Trigger event ID of one issue version: a hash of the site, the issue ID, and the updated
 * instant, so a refetch after a crash yields the same ID.
 */
export function issueChangeEventId(cloudId: string, issue: Issue): string {
  const version = [cloudId, issue.id, String(unixMilliseconds(issue.updatedAt))].join('\u0000');
  return 'jira-' + createHash('sha256').update(version, 'utf8').digest().subarray(0, 16).toString('hex');
}

/** Convert one decoded search result into its Trigger event. */
export function newIssueChangedTriggerEvent(cloudId: string, issue: Issue): TriggerEvent<IssueChangedEvent> {
  const created = unixMilliseconds(issue.createdAt), updated = unixMilliseconds(issue.updatedAt);
  const isCreated = created !== zeroTimeMillis && Math.abs(updated - created) <= createdUpdatedToleranceMs;
  return { id: issueChangeEventId(cloudId, issue), occurredAt: new Date(updated), payload: { cloudId, changeKind: isCreated ? 'created' : 'updated', issue } };
}

/**
 * Reads one binding's issue changes from Jira's enhanced search for the poll Trigger kit. A poller owns it and
 * calls it one request at a time. It estimates Jira's clock from each search response's Date header, so a
 * skewed host clock cannot hide a change.
 */
export class IssueChangeFetcher {
  private readonly projectClause: string;
  private readonly projectIdentity: string;
  private jiraClockOffsetMs = 0;

  /** Bind the client, connection, and watched project; validate the configuration beforehand. */
  constructor(private readonly client: JiraClient, private readonly connection: ConnectionRef, configuration: IssueChangedTriggerConfiguration) {
    const [clauseValue, identity] = watchedProject(configuration);
    this.projectClause = clauseValue ? `project = ${clauseValue} AND ` : '';
    this.projectIdentity = identity;
  }

  /** Record Jira's current time, or a later updated time Jira reports, as the baseline; recent issues are marked seen. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const session = await this.startPollSession(context);
    const minutes = issueChangeLookbackMs / 60_000 + 2;
    const jql = `${this.projectClause}updated >= -${minutes}m ORDER BY updated DESC`;
    const versions: [string, number][] = [];
    let pageToken = '';
    for (let page = 0; page < maximumSeedPages; page++) {
      const listed = await this.searchPage(context, session, jql, ['updated'], pageToken);
      if (listed.isPageTokenRejected) throw new Error('Jira rejected the baseline search page token');
      for (const resource of listed.issues) versions.push(decodeIssueVersion(resource));
      if (!listed.nextPageToken || listed.nextPageToken === pageToken) break;
      pageToken = listed.nextPageToken;
    }
    // Read after the first response, so the baseline is on Jira's clock rather than the local one.
    const updatedThrough = Math.max(this.jiraNow(), ...versions.map(([, millis]) => millis));
    const position = newIssueChangeCursor(session.cloudId, this.projectIdentity, updatedThrough);
    position.seenVersions = recentSeenVersions([], versions, updatedThrough);
    return encodeIssueChangeCursor(position);
  }

  /**
   * Read one ascending page of issues updated since the cursor's lookback and return the unseen versions. An
   * undecodable cursor, or one for another site or project, is marked invalid, so the kit records a new
   * baseline. A rejected page token restarts the search from the committed position.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<IssueChangedEvent>>> {
    let position: IssueChangeCursor;
    try { position = decodeIssueChangeCursor(cursor); }
    catch (error) { throw markCursorInvalid(error); }
    if (position.project !== this.projectIdentity) throw markCursorInvalid(new Error('Jira issue cursor belongs to another project'));
    const session = await this.startPollSession(context);
    if (position.cloudId !== session.cloudId) throw markCursorInvalid(new Error('Jira issue cursor belongs to another site'));
    const now = this.jiraNow();
    // A relative JQL bound moves with Jira's clock; restart before it passes the committed position.
    if (position.pageToken && now - position.searchStartedAt > issueChangeLookbackMs) position.pageToken = '';
    let listed = await this.searchChangedIssues(context, session, position, now);
    if (listed.isPageTokenRejected) {
      position.pageToken = '';
      listed = await this.searchChangedIssues(context, session, position, now);
      if (listed.isPageTokenRejected) throw new Error('Jira rejected the issue search without a page token');
    }
    if (listed.issues.length > MaxChangesPerPage) throw new Error(`Jira issue search page has ${listed.issues.length} issues, more than ${MaxChangesPerPage}`);
    const seen = new Set(position.seenVersions);
    const threshold = position.updatedThrough - issueChangeLookbackMs;
    const delivered: [string, number][] = [];
    const changes: TriggerEvent<IssueChangedEvent>[] = [];
    let updatedThrough = position.updatedThrough;
    for (const resource of listed.issues) {
      let issue: Issue;
      try { issue = decodeIssue(resource, [], false); }
      catch (error) { throw new Error(`Jira issue search page has an invalid issue: ${error instanceof Error ? error.message : String(error)}`); }
      const updatedMillis = unixMilliseconds(issue.updatedAt);
      const key = issueVersionKey(issue.id, updatedMillis);
      if (updatedMillis < threshold || seen.has(key)) continue;
      seen.add(key);
      delivered.push([issue.id, updatedMillis]);
      updatedThrough = Math.max(updatedThrough, updatedMillis);
      changes.push(newIssueChangedTriggerEvent(session.cloudId, issue));
    }
    const next = newIssueChangeCursor(position.cloudId, position.project, updatedThrough);
    next.seenVersions = recentSeenVersions(position.seenVersions, delivered, updatedThrough);
    let hasMorePages = false;
    if (listed.nextPageToken && listed.nextPageToken !== position.pageToken) {
      hasMorePages = true;
      // A fresh search from the new floor re-reads any issue an update moved across the page boundary.
      if (updatedThrough - issueChangeLookbackMs < position.searchFloor + pageChainProgressMs) {
        Object.assign(next, { pageToken: listed.nextPageToken, searchMinutes: position.searchMinutes, searchStartedAt: position.searchStartedAt, searchFloor: position.searchFloor });
      }
    }
    return { changes, next: encodeIssueChangeCursor(next), hasMorePages };
  }

  /** Read the cursor's page, or start a search covering the lookback plus one minute of JQL precision; the relative bound ignores the account time zone. */
  private async searchChangedIssues(context: TriggerContext, session: JiraSession, position: IssueChangeCursor, now: number): Promise<SearchPage> {
    if (!position.pageToken) {
      const since = position.updatedThrough - issueChangeLookbackMs;
      position.searchMinutes = Math.floor(Math.max(now - since, 0) / 60_000) + 2;
      position.searchStartedAt = now;
      position.searchFloor = since;
    }
    const jql = `${this.projectClause}updated >= -${position.searchMinutes}m ORDER BY updated ASC`;
    return this.searchPage(context, session, jql, standardFields, position.pageToken);
  }

  private async startPollSession(context: TriggerContext): Promise<JiraSession> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, IssueChangedTriggerDefinition.trigger, context.signal);
    let credentialSession: JiraSession;
    try { credentialSession = await this.client.resolveSessionCredentials(call, operation, context.signal); }
    catch { throw new Error('Jira Trigger credentials are unavailable; reconnect the connection if this persists'); }
    try { return await this.client.resolveSite(credentialSession, operation); }
    catch (error) {
      if (!(error instanceof JiraSessionFailure)) throw error;
      const siteError = new Error(error.failure.message);
      throw error.failure.kind === 'RATE_LIMIT' ? markRateLimited(siteError, error.retryAfterMs) : siteError;
    }
  }

  /** Send one search request; a 429 pauses the connection, and a 400 for a page token reports the token as rejected. */
  private async searchPage(context: TriggerContext, session: JiraSession, jql: string, fields: string[], pageToken: string): Promise<SearchPage> {
    await waitForPollRequest(context);
    const body = { jql, fields, maxResults: pageSize, ...(pageToken ? { nextPageToken: pageToken } : {}) };
    const exchange = await this.client.exchange(session, 'POST', searchPath, undefined, body, context.signal);
    if ('error' in exchange) throw new Error('Jira issue search is temporarily unavailable');
    const response = exchange.response;
    const date = Date.parse(firstHeader(response.headers, 'date'));
    if (Number.isFinite(date)) this.jiraClockOffsetMs = date - this.client.now().getTime();
    if (response.status === 429) {
      // The hourly points quota answers 429 with Retry-After until the window resets.
      throw markRateLimited(new Error('Jira rate limited the issue search'), retryAfter(response));
    }
    if (response.status === 503 && firstHeader(response.headers, 'retry-after')) {
      // Jira documents Retry-After on some transient 503 responses; honor it like a rate limit.
      throw markRateLimited(new Error('Jira asked the issue search to wait'), retryAfter(response));
    }
    if (response.status === 400 && pageToken) return { issues: [], nextPageToken: '', isPageTokenRejected: true };
    if (response.status < 200 || response.status >= 300) throw new Error(rejection(operation, searchSubject, response.status, rejectedFields(response.body)).message);
    if (exchange.isTooLarge) throw new Error('Jira issue search response exceeds maxResponseBytes');
    if (exchange.reflectsSecret) throw new Error('Jira issue search response contains credential material');
    let issues: unknown[], nextPageToken: string;
    try {
      const page = decodeObject(response.body);
      if (page.issues != null && (!Array.isArray(page.issues) || page.issues.some(issue => !issue || typeof issue !== 'object' || Array.isArray(issue)))) throw new TypeError('issues');
      if (page.nextPageToken != null && typeof page.nextPageToken !== 'string') throw new TypeError('nextPageToken');
      issues = (page.issues ?? []) as unknown[];
      nextPageToken = (page.nextPageToken ?? '') as string;
    } catch { throw new Error('Jira returned an invalid issue search page'); }
    if (Buffer.byteLength(nextPageToken, 'utf8') > maximumPageTokenBytes || !/^[!-~]*$/.test(nextPageToken)) throw new Error('Jira returned an invalid issue search page');
    return { issues, nextPageToken, isPageTokenRejected: false };
  }

  /** Estimate Jira's clock, in Unix milliseconds, which evaluates relative JQL and stamps updated times. */
  private jiraNow(): number { return this.client.now().getTime() + this.jiraClockOffsetMs; }
}

function decodeIssueVersion(resource: unknown): [string, number] {
  const invalid = () => new Error('Jira baseline search returned an issue without a valid ID and updated time');
  if (!resource || typeof resource !== 'object' || Array.isArray(resource)) throw invalid();
  const item = resource as Record<string, unknown>;
  const fields = item.fields == null ? {} : item.fields;
  if (typeof fields !== 'object' || Array.isArray(fields) || (item.id != null && typeof item.id !== 'string')) throw invalid();
  let updated: string;
  try { updated = parseJiraTime((fields as Record<string, unknown>).updated); } catch { throw invalid(); }
  const issueId = (item.id ?? '') as string;
  if (unixMilliseconds(updated) === zeroTimeMillis || !/^[1-9][0-9]{0,17}$/.test(issueId)) throw invalid();
  return [issueId, unixMilliseconds(updated)];
}
