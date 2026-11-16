// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  PageChangeCreated, PageChangeUpdated,
  definePageChangedTriggerBinding, newGetPageStep, type Connection, type GetPageInput, type Page, type PageChangeKind, type PageChangedEvent,
} from '@superdurable/connector-atlassian-confluence';

/** Stable Flow identity each page change starts. */
export const FlowType = 'ConfluencePageChangeTypeScript';
/** Static Dex Web connection for Confluence. */
export const ConnectionName = 'confluence-page-watcher';
/** The pageChanged binding that starts this Flow. */
export const PageChangedTriggerBinding = 'space-page-changes';
/** Bounds the Markdown kept in the outcome. */
export const MaxRecordedBodyCharacters = 2000;

/** One pageChanged delivery and the stable Trigger event ID that also names the Flow. */
export interface PageChangeInput { /** Trigger event ID. */ triggerEventId: string; /** The delivered change. */ change: PageChangedEvent }
/** The Flow result: the delivered change and the page read back from Confluence. */
export interface PageChangeOutcome {
  /** The changed page. */ pageId: string;
  /** The delivered change kind. */ changeKind: PageChangeKind;
  /** The version the Trigger delivered. */ deliveredVersion: number;
  /** The version read back; later than deliveredVersion when the page changed again. */ currentVersion: number;
  /** The page title read back. */ title: string;
  /** The start of the page body as Markdown, at most MaxRecordedBodyCharacters characters. */ body: string;
  /** The page's address in the Confluence web UI. */ webUrl?: string;
}

/** The delivered change. */
export const pageChangeAttribute = new Attribute<PageChangeInput>('confluence-page-change', jsonCodec<PageChangeInput>());
/** The page read back from Confluence. */
export const pageChangeOutcomeAttribute = new Attribute<PageChangeOutcome>('confluence-page-change-outcome', jsonCodec<PageChangeOutcome>());

/** Records one Confluence page change and reads the page back. */
export class PageChangeFlow implements Flow<PageChangeInput> {
  private readonly record = new RecordPageChange();
  private readonly complete = new CompletePageChange();
  /** Bind the Confluence connection at registration time. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Confluence connection is required'); }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return FlowType; }
  /** Register the record, read, and completion Steps. */
  getSteps(): StepList<PageChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetPageStep<PageChangeInput>({
        stepType: 'ReadChangedPage', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed page back as Markdown, so the record holds its current title and body.' },
        mapToOperationInput: mapToGetPageInput, found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [pageChangeAttribute, pageChangeOutcomeAttribute] }; }
  /** Declare the pageChanged binding and its space picker for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [definePageChangedTriggerBinding({ connectionName: ConnectionName, bindingName: PageChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedSpace', unitId: 'spacePicker', label: 'Watched space', required: false,
      description: "Choose the Confluence space whose created and updated pages start this Flow; the list comes from the current spaces this connection's site lets the account view, and the picker saves the space ID, key, and name, which are not secret and are stored in the project configuration. The same spaces are listed at https://<your-site>.atlassian.net/wiki/spaces, and a key looks like OPS. Leave it unsaved to watch every space the account can view. The first start records recently changed pages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed space records a new baseline.",
      bindings: [
        { port: 'spaceId', jsonPointer: '/spaceId' },
        { port: 'spaceKey', jsonPointer: '/spaceKey' },
        { port: 'spaceName', jsonPointer: '/spaceName' },
      ],
    }] } })];
  }
}

/** Name one Flow per page version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<PageChangedEvent>): string { return `confluence-page-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<PageChangedEvent>): PageChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to new pages. */
export function acceptEveryPageChange(): boolean { return true; }
/** Read the changed page as Markdown, bounded for the record. */
export function mapToGetPageInput(input: PageChangeInput): GetPageInput { return { pageId: input.change.page.id, maxBodyCharacters: MaxRecordedBodyCharacters }; }

/** Return why a delivered change is unusable, or an empty string. */
export function validatePageChange(input: PageChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.page?.id?.trim() || (input.change.page.versionNumber ?? 0) < 1) return 'change.page.id and change.page.versionNumber are required';
  if (input.change.changeKind !== PageChangeCreated && input.change.changeKind !== PageChangeUpdated) return 'change.changeKind must be created or updated';
  return '';
}

/** Validate and record the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered page change, then read the page back."
export class RecordPageChange implements Step<PageChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordPageChange'; }
  /** Fail an unusable change; otherwise record it and read the page. */
  execute(context: Context, input: PageChangeInput): StepDecision {
    const problem = validatePageChange(input);
    if (problem) return forceFail(problem);
    pageChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedPage'), input);
  }
}

/** Record the page read back as the outcome. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the page read back from Confluence as the outcome and complete the Flow."
export class CompletePageChange implements Step<QueryResult<Page>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompletePageChange'; }
  /** Save the outcome and complete with it. */
  execute(context: Context, result: QueryResult<Page>): StepDecision {
    const { change } = pageChangeAttribute.get(context);
    const current = result.value;
    const outcome: PageChangeOutcome = {
      pageId: change.page.id, changeKind: change.changeKind, deliveredVersion: change.page.versionNumber ?? 0,
      currentVersion: current.version.number, title: current.title, body: current.body, ...(current.webUrl ? { webUrl: current.webUrl } : {}),
    };
    pageChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
