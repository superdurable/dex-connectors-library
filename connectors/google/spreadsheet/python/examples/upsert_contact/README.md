# Upsert contact with Google Sheets

This Flow uses the `google-sheets-contacts` project connection, reads the `email`, `name`, and `status` columns, and upserts a contact by email. Configure the Google Sheets OAuth connection in Dex Web as described in the [connector README](../../../README.md), then start a local Dex server and run `uv run python -m examples.upsert_contact.main` from `connectors/google/spreadsheet/python`.

Start `GoogleSheetsUpsertContactPython` with a unique Flow ID and this JSON input:

```json
{"spreadsheetId":"your-spreadsheet-id","sheetName":"Contacts","email":"ada@example.test","name":"Ada","status":"active"}
```

Only `upserted` is wired. A conflict, provider rejection, invalid response, uncertain write, or local defect fails the Flow. `integration_test.py` verifies retry and routing through a real local Dex Worker and Client with an inert provider stand-in.
