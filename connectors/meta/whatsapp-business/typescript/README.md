# WhatsApp Business TypeScript connector

This package lists business phone numbers and sends text or template messages through the Meta Cloud API. Sends use a durable single-dispatch fence: an unknown outcome resends while possible sends stay below the send cap and selects `uncertain` at the cap, and a definite refusal is never resent. The package replays Go-recorded provider scenarios and includes runnable Dex Flows under `examples/`.

Configure the static `whatsapp-business` connection with the versioned Graph API endpoint and a secret system-user access token. Live provider authorization remains unverified without a user-controlled connection.
