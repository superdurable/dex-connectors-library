// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchStoriesStep, type Connection, type SearchStoriesInput, type StorySearchHit, type StorySearchPage } from '../../src/index.js';

/** Static Dex Web connection name used by the example. */
export const ConnectionName = 'hackernews-public';

/** Starts one search. Only query is required. */
export interface StorySearchRequest {
  /** Keyword query, such as "postgres" or "rust compiler"; it must not be blank. */
  query: string;
  /** Keeps stories submitted in the last 1 through 720 hours; omitted selects 24. */
  withinHours?: number;
  /** Narrows the search to show_hn, ask_hn, or front_page stories; omitted searches all stories. */
  tags?: SearchStoriesInput['tags'];
  /** Keeps stories with at least this many points; omitted sets no minimum. */
  minPoints?: number;
  /** date reads the newest stories first and relevance the best matches first; omitted selects date. */
  sortBy?: 'date' | 'relevance';
  /** Reads at most 1 through 20 pages of 50 stories; omitted selects 4. */
  maxPages?: number;
}

/** The search result, persisted for Dex Web and returned on completion. */
export interface StorySearchReport {
  /** The start request with defaults applied. */
  request: StorySearchRequest;
  /** The fixed start of the search window, chosen when the Flow started. */
  createdAfter: string;
  /** Each matching story once, in the order the pages returned them. */
  stories: StorySearchHit[];
  /** The latest page's count of matching stories, which can be an estimate. */
  totalHits: number;
  /** The number of result pages read. */
  pagesRead: number;
  /** Whether maxPages stopped the search before the last result page. */
  hasUnreadPages: boolean;
}

const searchReport = new Attribute<StorySearchReport>('hackernews-story-search-report', jsonCodec<StorySearchReport>());
const storiesPerPageRead = 50;

/** Search Hacker News once and complete with the matching stories. */
export class StorySearchFlow implements Flow<StorySearchRequest> {
  private readonly start = new StartStorySearch();
  private readonly recordPage = new RecordStoryPage();
  private readonly search;

  /** Bind the declared named public connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('story-search Flow requires its static connection');
    this.search = newSearchStoriesStep<SearchStoriesInput>({
      stepType: 'SearchStories',
      annotations: { groupId: 'search', groupLabel: 'Search', explanation: 'Read one page of matching stories from Hacker News Search.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input, searched: this.recordPage,
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'HackerNewsStorySearch'; }
  /** Register request validation, the search Step, and page collection. */
  getSteps(): StepList<StorySearchRequest> {
    return StepList.startStep<StorySearchRequest>(this.start).otherSteps(this.search, this.recordPage);
  }
  /** Register the search report. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [searchReport] }; }
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Validate the request and fix the search window before reading the first page."
class StartStorySearch implements Step<StorySearchRequest> {
  readonly inputCodec = jsonCodec<StorySearchRequest>();
  getStepType(): string { return 'StartStorySearch'; }
  execute(context: Context, request: StorySearchRequest): StepDecision {
    const resolved = resolveRequest(request);
    if (typeof resolved === 'string') return forceFail(resolved);
    const windowStart = Math.floor(Date.now() / 1000) - resolved.withinHours! * 3600;
    const report: StorySearchReport = {
      request: resolved, createdAfter: new Date(windowStart * 1000).toISOString().replace('.000Z', 'Z'), stories: [], totalHits: 0, pagesRead: 0, hasUnreadPages: false,
    };
    searchReport.set(context, report);
    return goTo(new StepRef('SearchStories'), searchInput(report, 0));
  }
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Keep each new story once and read the next page until the results or the page budget run out."
class RecordStoryPage implements Step<QueryResult<StorySearchPage>> {
  readonly inputCodec = jsonCodec<QueryResult<StorySearchPage>>();
  getStepType(): string { return 'RecordStoryPage'; }
  execute(context: Context, result: QueryResult<StorySearchPage>): StepDecision {
    const report = searchReport.get(context);
    const page = result.value;
    report.stories = appendNewStories(report.stories, page.stories ?? []);
    report.totalHits = page.totalHits;
    report.pagesRead++;
    report.hasUnreadPages = page.nextPage !== 0 && report.pagesRead >= report.request.maxPages!;
    searchReport.set(context, report);
    if (page.nextPage !== 0 && !report.hasUnreadPages) return goTo(new StepRef('SearchStories'), searchInput(report, page.nextPage));
    return gracefulComplete(report);
  }
}

/** Apply defaults and bounds; a string is the reason the request is invalid. */
export function resolveRequest(request: StorySearchRequest): StorySearchRequest | string {
  if (!request.query?.trim()) return 'query is required';
  const resolved: StorySearchRequest = { ...request, withinHours: request.withinHours || 24, maxPages: request.maxPages || 4, sortBy: request.sortBy || 'date' };
  if (!Number.isInteger(resolved.withinHours) || resolved.withinHours! < 1 || resolved.withinHours! > 720 || !Number.isInteger(resolved.maxPages) || resolved.maxPages! < 1 || resolved.maxPages! > 20) {
    return 'withinHours must be 1..720 and maxPages must be 1..20';
  }
  return resolved;
}

/** Build one page's input; every page keeps the window fixed at start. */
export function searchInput(report: StorySearchReport, page: number): SearchStoriesInput {
  const { query, sortBy, tags, minPoints } = report.request;
  const input: SearchStoriesInput = { query, sortBy, createdAfter: report.createdAfter, page, hitsPerPage: storiesPerPageRead };
  if (tags?.length) input.tags = tags;
  if (minPoints) input.minPoints = minPoints;
  return input;
}

// A newest-first page can repeat a story when newer stories arrive between reads.
function appendNewStories(kept: StorySearchHit[], page: StorySearchHit[]): StorySearchHit[] {
  const keptIds = new Set(kept.map(story => story.id));
  const stories = [...kept];
  for (const story of page) {
    if (keptIds.has(story.id)) continue;
    keptIds.add(story.id);
    stories.push(story);
  }
  return stories;
}
