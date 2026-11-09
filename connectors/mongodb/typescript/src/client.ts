// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { MongoClient, type MongoClientOptions } from 'mongodb';
import type { CredentialProvider } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { FindDocumentsOperation } from './operations.js';

/** Native driver factory for deterministic provider tests. */
export interface MongoDBClientOptions {
  /** Open one short-lived client for each Dex Step attempt. */
  createDriver?: (uri: string, options: MongoClientOptions) => MongoClient;
}

/** Validated read bounds and a per-attempt rotating URI source. */
export class MongoDBClient {
  readonly configuration: Required<Config>;
  readonly credentials: CredentialProvider<Credentials>;
  readonly createDriver: (uri: string, options: MongoClientOptions) => MongoClient;

  /** Validate settings without contacting MongoDB. */
  constructor(configuration: Config, credentials: CredentialProvider<Credentials>, options: MongoDBClientOptions = {}) {
    if (!configuration || !credentials) throw new TypeError('MongoDB configuration and credential source are required');
    const selected = {
      database: configuration.database,
      timeoutMs: configuration.timeoutMs ?? 15_000,
      maxDocuments: configuration.maxDocuments ?? 100,
      maxResponseBytes: configuration.maxResponseBytes ?? 1_048_576,
    };
    if (!/^[A-Za-z_][A-Za-z0-9_-]*$/u.test(selected.database)) throw new TypeError('MongoDB database name is invalid');
    if (!Number.isSafeInteger(selected.timeoutMs) || selected.timeoutMs < 1 ||
        !Number.isSafeInteger(selected.maxDocuments) || selected.maxDocuments < 1 || selected.maxDocuments > 1000 ||
        !Number.isSafeInteger(selected.maxResponseBytes) || selected.maxResponseBytes < 1 || selected.maxResponseBytes > 16 * 1024 * 1024)
      throw new TypeError('MongoDB read bounds are invalid');
    this.configuration = selected;
    this.credentials = credentials;
    this.createDriver = options.createDriver ?? ((uri, driverOptions) => new MongoClient(uri, driverOptions));
  }

  /** Bind the read-only document Query. */
  findDocuments(): FindDocumentsOperation { return new FindDocumentsOperation(this); }
}
