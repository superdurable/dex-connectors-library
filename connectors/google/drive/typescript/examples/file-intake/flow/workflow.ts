// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineFileCreatedTriggerBinding, newGetFileStep, type Connection, type File, type FileEvent } from '@superdurable/connector-google-drive';

/** Static Dex Web connection for Google Drive. */
export const ConnectionName = 'google-drive-files';
/** The fileCreated binding that starts the Flow. */
export const NewFileTriggerBinding = 'google-drive-file-intake-new-file';
const readNewFileStepType = 'ReadNewFile';

/** The Flow start input that the fileCreated Trigger maps from one event. */
export interface FileIntakeInput {
  /** The Trigger event ID, such as 1AbC@created. */ eventId: string;
  /** The file metadata the change log reported. */ file: FileEvent;
}

/** The durable result of one intake and the Flow's completion output. */
export interface FileIntakeOutcome {
  /** The Trigger event that started the Flow. */ eventId: string;
  /** The metadata the change log reported. */ reported: FileEvent;
  /** The metadata read from Drive when the Flow ran; omitted until it is read. */ current?: File;
}

/** The intake outcome, set when the Flow starts and completed with the current metadata. */
export const outcomeAttribute = new Attribute<FileIntakeOutcome>('google-drive-typescript-file-intake-outcome', jsonCodec<FileIntakeOutcome>());

/** Handles one newly created Drive file. */
export class FileIntakeFlow implements Flow<FileIntakeInput> {
  private readonly recordNewFile = new RecordNewFile();
  private readonly completeFileIntake = new CompleteFileIntake();
  /** Bind the Google Drive connection used by the metadata read. */
  constructor(private readonly connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('Google Drive file intake requires the static connection');
  }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'GoogleDriveFileIntakeTypeScript'; }
  /** Register the record, Google Drive read, and completion Steps. */
  getSteps(): StepList<FileIntakeInput> {
    return StepList.startStep(this.recordNewFile).otherSteps(
      newGetFileStep<FileIntakeInput>({
        stepType: readNewFileStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'google-drive', groupLabel: 'Google Drive', explanation: "Read the new file's current metadata from Drive." },
        mapToOperationInput: mapToGetFileInput, found: this.completeFileIntake,
      }),
      this.completeFileIntake,
    );
  }
  /** Register the outcome Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [outcomeAttribute] }; }
  /**
   * Declare the fileCreated binding and its Dex Web folder and file type units. Dex CLI v1.47.3 reads the
   * configuration UI only from string literals, so the unit and port IDs are not the package's constants.
   */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineFileCreatedTriggerBinding({ connectionName: ConnectionName, bindingName: NewFileTriggerBinding, configurationUI: { units: [
      {
        id: 'folder', unitId: 'folderPicker', label: 'Watched folder', required: false,
        description: 'Choose the folder whose new files start the intake Flow; only files directly inside it count, not files in its subfolders. Leave blank to start a Flow for every new file the connection can see.',
        bindings: [{ port: 'folderId', jsonPointer: '/folderId' }, { port: 'folderName', jsonPointer: '/folderName' }],
      },
      {
        id: 'mimeType', unitId: 'mimeTypePicker', label: 'File type', required: false,
        description: 'Choose the exact file type that starts the intake Flow, such as PDF for scanned invoices; leave blank to start a Flow for new files of every type, including folders.',
        bindings: [{ port: 'mimeType', jsonPointer: '/mimeType' }],
      },
    ] } })];
  }
}

/** Derive one Flow ID per created file, so a redelivered event starts no second Flow. */
export function resolveFlowId(event: TriggerEvent<FileEvent>): string { return `google-drive-typescript-file-intake-${ConnectionName}-${event.payload.fileId}`; }
/** Map one fileCreated event to the Flow start input. */
export function mapToFlowInput(event: TriggerEvent<FileEvent>): FileIntakeInput { return { eventId: event.id, file: event.payload }; }
/** Accept the binding's fileCreated events, which the folder and type filters already matched. */
export function acceptCreatedFile(event: TriggerEvent<FileEvent>): boolean { return event.id.endsWith('@created'); }
/** Read the reported file. */
export function mapToGetFileInput(input: FileIntakeInput) { return { fileId: input.file.fileId }; }

/** Persist the reported metadata before the provider read. */
// dex:group group-id:file-intake-receive group-label:"Receive file"
// dex:explanation text:"Record the new file that the fileCreated Trigger reported."
export class RecordNewFile implements Step<FileIntakeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordNewFile'; }
  /** Record the outcome and read the file's current metadata. */
  execute(context: Context, input: FileIntakeInput): StepDecision {
    outcomeAttribute.set(context, { eventId: input.eventId, reported: input.file });
    return goTo(new StepRef('ReadNewFile'), input);
  }
}

/** Complete with the reported and current metadata. */
// dex:group group-id:file-intake-complete group-label:"Complete intake"
// dex:explanation text:"Record the file's current metadata and complete the Flow."
export class CompleteFileIntake implements Step<QueryResult<File>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteFileIntake'; }
  /** Save the current metadata and return the outcome. */
  execute(context: Context, result: QueryResult<File>): StepDecision {
    const outcome: FileIntakeOutcome = { ...outcomeAttribute.get(context), current: result.value };
    outcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
