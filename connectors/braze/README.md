# Braze connector

The Go `getUserProfile` Query calls Braze `POST /users/export/ids` for one external user ID. It always requests only `external_id`, `braze_id`, `first_name`, `last_name`, and `email`; custom attributes and other profile fields are not returned by the connector. An empty `users` array selects `notFound`. Rate limits, transport failures, and server errors retry. The query never writes to Braze.

Create a REST API key with `users.export.ids` permission in Braze Dashboard > Settings > APIs and Identifiers > API Keys. Enter the account's REST Endpoint in `baseUrl` and the key in the secret `rest_api_key` field. See the [Braze API basics](https://www.braze.com/docs/api/basics) and [user export endpoint](https://www.braze.com/docs/api/endpoints/export/user_data/post_users_identifier).

The runnable Dex Flow is in [`go/examples/profile-inspection`](go/examples/profile-inspection/README.md). Its connection name is `braze`. The Go, Python, and TypeScript packages each declare their first `v0.1.0` release and share Go-recorded conformance scenarios.
