# RabbitMQ connector

The Go `getQueueMetrics` Query reads message and consumer counts for one queue through the [RabbitMQ Management HTTP API](https://www.rabbitmq.com/docs/http-api-reference#get-apiqueuesvhostname). It returns only the virtual host, queue name, message count, consumer count, and state; no messages or credentials enter Flow state. Python and TypeScript packages are pending.

Enable the `rabbitmq_management` plugin and obtain the TLS Management UI origin from your administrator. In Dex Web Connections enter that origin as `managementUrl`, without `/api`; enter a username with read permission for the virtual host and its secret password from **Admin > Users**. Blank settings or credentials prevent calls. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults. HTTPS is required except for loopback development. The default `guest` account is unsuitable for remote connections.

Pass the exact virtual host and queue name in Flow input. The default virtual host is `/`; it is percent encoded as `%2F` in the request path. A missing queue selects `notFound`; 429, 408, 5xx, and transport errors retry. The [Go example](go/examples/queue-inspection/README.md) uses the static `rabbitmq` connection and a real Dex integration test with a deterministic provider fixture. Real broker authorization and delivery have not been verified.
