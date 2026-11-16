// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { ConnectionRef, Failure, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { HubSpotClient } from './client.js';
import type { ObjectPage, SearchObjectsInput } from './generated/models.js';
import { decodeObjectPage, objectsPath, searchBody } from './operations.js';
import { CRMObjectChangeCreated, CRMObjectChangeUpdated, CRMObjectChangedTriggerDefinition, type CRMObjectChangeKind, type CRMObjectChangedEvent, type CRMObjectChangedTriggerConfiguration } from './crm_object_changed.js';
import { decodeCRMObjectChangeCursor, encodeCRMObjectChangeCursor, lagWindowStart, pruneSeen } from './crm_object_change_cursor.js';
import {
  MaximumSearchResults, crmObjectChangePageSize, formatGoTimeMilliseconds, lastModifiedProperty, newCRMObjectChangeSearch, recordCreatedMilliseconds, recordModifiedMilliseconds,
} from './crm_object_change_search.js';

const crmObjectChangeOperation = 'crmObjectChanged';

/** One unsuccessful search exchange; its message carries no record data. */
export class CRMObjectChangeSearchError extends Error {
  /** Wrap the classified failure of one search. */
  constructor(readonly failure: Failure) {
    super(`HubSpot change search: ${failure.message}`);
    this.name = 'CRMObjectChangeSearchError';
  }
}

/** Polls one object type through CRM search sorted by last-modified date. */
export class CRMObjectChangeFetcher implements ChangeFetcher<TriggerEvent<CRMObjectChangedEvent>> {
  /** Bind the client, the connection whose credentials each poll resolves, and the validated configuration. */
  constructor(private readonly client: HubSpotClient, private readonly connection: ConnectionRef, private readonly configuration: CRMObjectChangedTriggerConfiguration) {}

  /**
   * Read the newest last-modified date, so every existing record becomes the baseline. An object type without
   * records seeds at zero, so every later record is delivered.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const objectType = this.configuration.objectType;
    const modifiedProperty = lastModifiedProperty(objectType);
    const page = await this.search(context, { objectType, properties: [modifiedProperty], limit: 1, sort: { propertyName: modifiedProperty, direction: 'DESCENDING' } });
    const floor = page.objects?.[0] ? recordModifiedMilliseconds(page.objects[0]) : 0;
    return encodeCRMObjectChangeCursor({ objectType, floorMs: floor, highWaterMs: floor, anchorMs: floor + 1, after: '', seen: new Map(), droppedThroughMs: 0 });
  }

  /**
   * Read one search page after cursor. An undecodable cursor, a cursor for another object type, and a paging
   * offset HubSpot rejects are marked invalid, so the kit records a new baseline.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<CRMObjectChangedEvent>>> {
    let position;
    try { position = decodeCRMObjectChangeCursor(cursor.token); } catch (error) { throw markCursorInvalid(error); }
    if (position.objectType !== this.configuration.objectType) throw markCursorInvalid(new Error('HubSpot change cursor belongs to another object type'));
    let page: ObjectPage;
    try { page = await this.search(context, newCRMObjectChangeSearch(this.configuration, position.anchorMs, position.after)); }
    catch (error) {
      if (position.after && error instanceof CRMObjectChangeSearchError && error.failure.kind === 'VALIDATION') {
        throw markCursorInvalid(new Error(`HubSpot rejected the change search offset: ${error.message}`, { cause: error }));
      }
      throw error;
    }
    const next = { ...position, seen: new Map(position.seen) };
    const changes: TriggerEvent<CRMObjectChangedEvent>[] = [];
    let pageNewest = position.anchorMs;
    for (const object of page.objects ?? []) {
      const modified = recordModifiedMilliseconds(object);
      pageNewest = Math.max(pageNewest, modified);
      const delivered = next.seen.get(object.id);
      if ((delivered !== undefined && delivered >= modified) || modified <= position.floorMs) continue;
      const kind: CRMObjectChangeKind = recordCreatedMilliseconds(object) > position.highWaterMs ? CRMObjectChangeCreated : CRMObjectChangeUpdated;
      const modifiedAt = formatGoTimeMilliseconds(modified);
      changes.push({
        id: crmObjectChangeID(object.objectType, object.id, modified), occurredAt: new Date(modified),
        payload: { objectType: this.configuration.objectType, changeKind: kind, modifiedAt, object },
      });
      next.seen.set(object.id, modified);
      next.highWaterMs = Math.max(next.highWaterMs, modified);
    }
    pruneSeen(next);
    let hasMorePages = false;
    if (page.nextAfter && pageNewest > position.anchorMs) {
      // Re-anchoring beats offset paging: an edit moves a record to the end and would shift the offsets.
      next.anchorMs = pageNewest; next.after = ''; hasMorePages = true;
    } else if (page.nextAfter) {
      if (Number(page.nextAfter) + crmObjectChangePageSize > MaximumSearchResults) {
        throw markCursorInvalid(new Error(`more than ${MaximumSearchResults} HubSpot records share one last-modified time`));
      }
      next.after = page.nextAfter; hasMorePages = true;
    } else {
      next.anchorMs = lagWindowStart(next); next.after = '';
    }
    return { changes, next: encodeCRMObjectChangeCursor(next), hasMorePages };
  }

  /** Send one CRM search; 429, 423, and the daily limit pause the whole connection. */
  private async search(context: TriggerContext, input: SearchObjectsInput): Promise<ObjectPage> {
    let body: Record<string, unknown>;
    try { body = searchBody(input); }
    catch (error) { throw new Error(`hubspot crmObjectChanged configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, CRMObjectChangedTriggerDefinition.trigger, context.signal);
    const result = await this.client.exchange(call, crmObjectChangeOperation, '', 'POST', objectsPath(input.objectType, 'search'), undefined, body);
    if ('outcome' in result) {
      const failure = new CRMObjectChangeSearchError(result.outcome.failure);
      if (result.outcome.failure.kind === 'RATE_LIMIT') throw markRateLimited(failure, result.outcome.retryAfterMs);
      // The daily limit resets at midnight in the account's time zone; back off without a known wait.
      if (result.outcome.failure.kind === 'QUOTA_EXHAUSTED') throw markRateLimited(failure, 0);
      throw failure;
    }
    let page: ObjectPage;
    try { page = decodeObjectPage(input.objectType, result.response.body); }
    catch (error) { throw new Error(`HubSpot change search returned an invalid page: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const limit = input.limit ?? 0;
    if ((page.objects?.length ?? 0) > limit) throw new Error(`HubSpot change search returned ${page.objects?.length ?? 0} records, more than ${limit}`);
    return page;
  }
}

/** A pure function of the record version, so a refetch after a crash yields the same ID. */
export function crmObjectChangeID(objectType: string, objectID: string, modifiedMilliseconds: number): string {
  const digest = createHash('sha256').update([objectType, objectID, String(modifiedMilliseconds)].join('\u0000')).digest();
  return `hubspot-${digest.subarray(0, 16).toString('hex')}`;
}
