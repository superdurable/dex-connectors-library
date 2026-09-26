export interface SpreadsheetFile { id: string; name: string; }

export function parseSpreadsheetPage(value: Record<string, unknown>): {files: SpreadsheetFile[]; nextPageToken: string} {
  const files = array(value.files).flatMap((item) => record(item) && text(item.id) && text(item.name) ? [{id: item.id, name: item.name}] : []);
  return {files, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
}

export function parseSheetTabs(value: Record<string, unknown>): string[] {
  return array(value.sheets).flatMap((sheet) => {
    if (!record(sheet) || !record(sheet.properties) || !text(sheet.properties.title)) return [];
    return [sheet.properties.title];
  });
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
