# Jotform connector

The Go connector lists form IDs, titles, and statuses with `listForms` and retrieves one submission's ID, form ID, status, and creation timestamp with `getSubmission`. Submission answers are deliberately excluded because forms can collect secrets and personal data. The API key is sent in the `APIKEY` request header, never in the URL, Flow input, or result.

Create a read-only key in [Jotform My Account > API](https://www.jotform.com/myaccount/api), copy it into the Dex Web secret field, and revoke it there when retired. Use the API base URL for the account's [US, EU, or HIPAA region](https://api.jotform.com/docs/). Blank `baseUrl` uses the US default; blank timeout and response-size fields use their displayed defaults. The [form-inspection Flow](go/examples/form-inspection/README.md) is the runnable consumer.

Both reads retry transport failures, HTTP 429, request timeout, and server failures. `getSubmission` selects `notFound` on a definitive 404. Provider rejections and malformed or oversized responses select their own optional branches. An unwired optional branch fails the Flow. Python and TypeScript ports follow Go-verified scenarios after the Go release.
