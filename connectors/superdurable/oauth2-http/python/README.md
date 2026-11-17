# OAuth 2.0 HTTP Python connector

Python package for the Dex `sendRequest` and `postNonIdempotent` Mutations. See
the connector README for authorization, request settings, and branches.

`sendRequest` repeats every unknown outcome, so use it only for requests that
are safe to repeat. `postNonIdempotent` sends one POST per Step execution under
the Connector SDK 0.12.0 single-dispatch fence: a POST that may have reached
the API without a usable answer selects `uncertain` and is never sent again.
The [API request example](examples/api_request/README.md) runs both.
