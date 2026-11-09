# WhatsApp Business Python connector

This package lists business phone numbers and sends text or template messages through the Meta Cloud API. Sends use a durable single-dispatch fence; ambiguous provider outcomes return `uncertain` and must not be retried blindly. The package replays Go-recorded provider scenarios and includes runnable Dex Flows under `examples/`.

Configure the static `whatsapp-business` connection with the versioned Graph API endpoint and a secret system-user access token. Live provider authorization remains unverified without a user-controlled connection.
