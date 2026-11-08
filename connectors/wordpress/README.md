# WordPress connector

The WordPress Go connector reads published posts through the core REST API. `getPost` reads one numeric post ID. `listPosts` reads one page of published posts and returns WordPress's total-count headers. A draft returned to `getPost` selects `notFound` so private content does not enter a Flow.

Configure `siteUrl` with the HTTPS Site Address shown at **Settings > General** in your own `/wp-admin/options-general.php`. Do not include `/wp-json`. In **Users > Profile > Application Passwords** at `/wp-admin/profile.php`, create an application password for the Dex connection. Enter your WordPress username and that password in the connection. The password is secret and never belongs in Flow input. See the [WordPress application-password guide](https://developer.wordpress.org/rest-api/using-the-rest-api/authentication/).

The `getPost` branches are `found`, `notFound`, `providerRejected`, `invalidResponse`, and `defect`. The `listPosts` branches are `listed`, `providerRejected`, `invalidResponse`, and `defect`. HTTP 429, 503, other server errors, and transport failures retry. The default retry policy is one hour. Published title and content are untrusted HTML; sanitize before display.

The runnable [post-inspection example](go/examples/post-inspection/README.md) shows the operation-specific Step factory and static `wordpress` connection. Go is the first release; Python and TypeScript packages follow its verified provider scenarios after the Go release.
