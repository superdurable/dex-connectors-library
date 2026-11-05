# Mailbox search Flow

This example reads one IMAP mailbox through the static `email-support` Dex Web
connection. Configure `imapHost`, `imapPort`, and `imapSecurity` from the mail
provider's IMAP settings; configure the SMTP fields for the same connection,
even though this search Flow does not send mail. Create an app password and
enter it in the authorization form. The connector's [connection guide](../../../README.md#connection-setup)
links to Fastmail, iCloud Mail, and Yahoo Mail's settings and password pages.

From `connectors/superdurable/email/python`, install the package with `uv sync`,
start `dexcli dev`, and then run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run python -m examples.mailbox_search.main
```

Start `EmailMailboxSearchPython` with [sample-input.json](sample-input.json).
The Flow returns matching summaries newest first. A missing or rejected mailbox
selects an optional branch and fails this example Flow.

The [integration test](integration_test.py) uses a TLS IMAP stand-in on an
OS-assigned port and a real Dex Worker and Client. Its SMTP probe also verifies
that a temporary refusal clears the heartbeat claim and retries to a single
accepted submission. It does not require a live provider credential.

Generate strict FDG 2.0 from this package directory with the latest stable
Dex CLI:

```sh
dexcli visualize ./examples/mailbox_search/flow.py \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out /tmp/email-python-graphs/mailbox-search
```

The generated JSON must report `valid: true` with no blocking diagnostics.
