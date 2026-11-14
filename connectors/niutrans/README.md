# NiuTrans text translation

The Go connector calls the official [text translation API](https://niutrans.com/documents/contents/trans_detection) with a source string of up to 1000 Unicode characters. It returns only translated text and drops provider echoes. The API key is sent in the HTTPS form body and never returned by the Flow.

NiuTrans bills one point per 2000 source characters, with free daily points and a documented 5000-character maximum per request. This connector limits text to 1000 characters and retries at most once after a failed attempt because a translation may already have consumed points. QPS errors (`10001`), provider timeouts (`13008`), HTTP 429/5xx, and transport failures retry within that bound; input/quota/auth errors terminate.

[Run the translation Flow](go/examples/translate/README.md). Its mock is synthetic; live translation and billing have not been verified without a user-controlled API key in Dex Web Connections.
