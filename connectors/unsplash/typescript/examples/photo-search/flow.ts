// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchPhotosStep, type Connection, type PhotoPage, type SearchPhotosInput } from '../../src/index.js';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'unsplash';
/** Safe public photo result retained by the Flow. */
export const photoSearchResult = new Attribute<PhotoPage>('unsplash-photo-search-result', jsonCodec<PhotoPage>());
/** Search one page of public photos through a named connection. */
export class PhotoSearchFlow implements Flow<SearchPhotosInput> {
  private readonly prepare = new PreparePhotoSearch();
  private readonly complete = new CompletePhotoSearch();
  /** Bind the static connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('photo search requires the static Unsplash connection'); }
  /** Return the stable Flow identity. */ getFlowType(): string { return 'UnsplashTypeScriptPhotoSearch'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<SearchPhotosInput> {
    return StepList.startStep<SearchPhotosInput>(this.prepare).otherSteps(
      newSearchPhotosStep({ stepType: 'SearchUnsplashPhotos', annotations: { groupId: 'photo-search', groupLabel: 'Photo search', explanation: 'Search Unsplash for public photos matching the phrase.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, searched: this.complete }), this.complete,
    );
  }
  /** Register the persisted public result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [photoSearchResult] }; }
}
// dex:group group-id:photo-search group-label:"Photo search"
// dex:explanation text:"Pass the search phrase and page size to the Unsplash Query."
class PreparePhotoSearch implements Step<SearchPhotosInput> {
  getStepType(): string { return 'PrepareUnsplashPhotoSearch'; }
  execute(_context: Context, input: SearchPhotosInput): StepDecision { return goTo(new StepRef('SearchUnsplashPhotos'), input); }
}
// dex:group group-id:photo-search group-label:"Photo search"
// dex:explanation text:"Retain public photo URLs and attribution after the search."
class CompletePhotoSearch implements Step<QueryResult<PhotoPage>> {
  getStepType(): string { return 'CompleteUnsplashPhotoSearch'; }
  execute(context: Context, result: QueryResult<PhotoPage>): StepDecision { photoSearchResult.set(context, result.value); return gracefulComplete(result.value); }
}
