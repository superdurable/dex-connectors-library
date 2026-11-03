// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newGetObjectTextStep, newHeadObjectStep, newListObjectsStep, newPutObjectStep, type Connection, type ListObjectsOutput, type ObjectMetadata, type ObjectText, type PutObjectInput, type StoredObject } from '@superdurable/connector-amazon-s3';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'amazon-s3-report-archive';

/** Safe input for one report write and read-back. */
export interface ReportArchiveInput {
  /** S3 bucket selected for the report. */ bucket: string;
  /** Stable object key for the report. */ key: string;
  /** Nonsecret UTF-8 report text. */ text: string;
}

/** Complete report archive proof. */
export interface ReportArchiveOutput {
  /** Stored object identity and write outcome. */ stored: StoredObject;
  /** Provider metadata read after the write. */ metadata: ObjectMetadata;
  /** Bounded report text read back. */ text: ObjectText;
  /** First listing page containing the report. */ listing: ListObjectsOutput;
}

const archiveRequest = new Attribute<ReportArchiveInput>('s3-archive-request', jsonCodec<ReportArchiveInput>());
const storedObject = new Attribute<StoredObject>('s3-archive-stored-object', jsonCodec<StoredObject>());
const objectMetadata = new Attribute<ObjectMetadata>('s3-archive-object-metadata', jsonCodec<ObjectMetadata>());
const objectText = new Attribute<ObjectText>('s3-archive-object-text', jsonCodec<ObjectText>());

/** Write, inspect, read, and list one text report through four S3 Steps. */
export class ReportArchiveFlow implements Flow<ReportArchiveInput> {
  private readonly prepare = new PrepareArchive();
  private readonly stored = new ArchiveStored();
  private readonly inspected = new ArchiveInspected();
  private readonly read = new ArchiveRead();
  private readonly listed = new ArchiveListed();

  /** Bind the static S3 connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('report archive requires its static S3 connection');
  }

  /** Return the stable Flow type. */ getFlowType(): string { return 'AmazonS3ReportArchive'; }

  /** Register each provider Step and terminal Step. */
  getSteps(): StepList<ReportArchiveInput> {
    return StepList.startStep<ReportArchiveInput>(this.prepare).otherSteps(
      newPutObjectStep<PutObjectInput>({ stepType: 'StoreReport', annotations: { groupId: 's3-archive', groupLabel: 'Report archive', explanation: 'Store the report with a create-only, retry-safe S3 write.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, stored: this.stored }),
      this.stored,
      newHeadObjectStep<{ bucket: string; key: string }>({ stepType: 'InspectReport', annotations: { groupId: 's3-archive', groupLabel: 'Report archive', explanation: 'Inspect the stored report metadata.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.inspected }),
      this.inspected,
      newGetObjectTextStep<{ bucket: string; key: string }>({ stepType: 'ReadReport', annotations: { groupId: 's3-archive', groupLabel: 'Report archive', explanation: 'Read the bounded report text.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, read: this.read }),
      this.read,
      newListObjectsStep<{ bucket: string; prefix: string }>({ stepType: 'ListReports', annotations: { groupId: 's3-archive', groupLabel: 'Report archive', explanation: 'List the report under its key prefix.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.listed }),
      this.listed,
    );
  }

  /** Register every durable report value. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [archiveRequest, storedObject, objectMetadata, objectText] }; }
}

// dex:group group-id:s3-archive group-label:"Report archive"
// dex:explanation text:"Validate safe report input and prepare a create-only object write."
class PrepareArchive implements Step<ReportArchiveInput> {
  getStepType(): string { return 'PrepareArchive'; }
  execute(context: Context, input: ReportArchiveInput): StepDecision {
    if (!input.bucket || !input.key || !input.text) return forceFail('bucket, key, and text are required');
    archiveRequest.set(context, input);
    return goTo(new StepRef('StoreReport'), { bucket: input.bucket, key: input.key, contentType: 'text/plain; charset=utf-8', textContent: input.text, isCreateOnly: true });
  }
}

// dex:group group-id:s3-archive group-label:"Report archive"
// dex:explanation text:"Keep the stored object identity and inspect its provider metadata."
class ArchiveStored implements Step<MutationResult<StoredObject>> {
  getStepType(): string { return 'ArchiveStored'; }
  execute(context: Context, result: MutationResult<StoredObject>): StepDecision {
    storedObject.set(context, result.value);
    return goTo(new StepRef('InspectReport'), { bucket: result.value.bucket, key: result.value.key });
  }
}

// dex:group group-id:s3-archive group-label:"Report archive"
// dex:explanation text:"Keep object metadata and read back the report's text."
class ArchiveInspected implements Step<QueryResult<ObjectMetadata>> {
  getStepType(): string { return 'ArchiveInspected'; }
  execute(context: Context, result: QueryResult<ObjectMetadata>): StepDecision {
    objectMetadata.set(context, result.value);
    return goTo(new StepRef('ReadReport'), { bucket: result.value.bucket, key: result.value.key });
  }
}

// dex:group group-id:s3-archive group-label:"Report archive"
// dex:explanation text:"Keep bounded text and list its containing prefix."
class ArchiveRead implements Step<QueryResult<ObjectText>> {
  getStepType(): string { return 'ArchiveRead'; }
  execute(context: Context, result: QueryResult<ObjectText>): StepDecision {
    objectText.set(context, result.value);
    const request = archiveRequest.get(context);
    return goTo(new StepRef('ListReports'), { bucket: request.bucket, prefix: request.key });
  }
}

// dex:group group-id:s3-archive group-label:"Report archive"
// dex:explanation text:"Complete with the stored object, metadata, text, and listing."
class ArchiveListed implements Step<QueryResult<ListObjectsOutput>> {
  getStepType(): string { return 'ArchiveListed'; }
  execute(context: Context, result: QueryResult<ListObjectsOutput>): StepDecision {
    const output: ReportArchiveOutput = { stored: storedObject.get(context), metadata: objectMetadata.get(context), text: objectText.get(context), listing: result.value };
    return gracefulComplete(output);
  }
}
