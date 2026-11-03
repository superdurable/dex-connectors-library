# Email connector for Python

Install `dex-connector-superdurable-email==0.1.0`. The package implements all six
email operations in the manifest: search, read, send, reply, move, and set flags.
It uses verified TLS 1.2 or later for IMAP and SMTP, including STARTTLS when
selected in the connection. Credentials are resolved on each operation attempt.

The [mailbox search example](examples/mailbox_search/README.md) is a runnable Dex
Flow with a static `email-support` connection. It uses `new_search_messages_step`
and completes with `SearchMessagesOutput`. Other operation-specific factories
are exported from `dex_connectors.superdurable_email`.

SMTP sends claim a Dex heartbeat before writing the message. If the final-dot
answer is lost, the `uncertain` result contains the stable Message-ID and a
later attempt does not resubmit. A definitive temporary refusal clears the
claim and retries. IMAP flag changes use absolute values; moves look for the
same Message-ID in the destination after a lost answer.

Provider protocol unit tests mirror the Go connector's mailtest scenarios.
`tests/integration/test_greenmail.py` runs verified SMTPS and IMAPS against the
CI GreenMail service. The example's `integration_test.py` runs against a real
Dex Worker and Client with a certificate-verified local IMAP stand-in.
Attachment names, part paths, and encoded sizes come from IMAP BODYSTRUCTURE,
including for messages with large bodies. Text is fetched separately and bounded
to the same 64 KiB decoded result as the Go connector. Live mailbox-provider
behavior has not been verified.
