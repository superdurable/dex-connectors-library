# Google Sheets Python Connector

This package exposes `getValues`, `findRow`, and `upsertRow` as Dex Python Steps. Install `dex-connector-google-spreadsheet==0.1.0` and create a `google-sheets-oauth` project connection. See [the connector README](../README.md) for Google Cloud OAuth setup and the append safety model.

The `examples/upsert_contact` application reads or upserts a contact in a selected spreadsheet. Run its integration test with a local Dex development server; live Google authorization and provider behavior have not been verified for this port.
