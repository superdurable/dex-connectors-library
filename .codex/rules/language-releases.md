# Python and TypeScript connector releases

`AGENTS.md` owns repository release rules. Keep Python packages in `sdkpython/`
or a connector's `python/` directory, and TypeScript packages in `sdkts/` or a
connector's `typescript/` directory. Their manifest language versions, package
metadata, and tags are independent of Go and each other. Release the matching
SDK first. Use `sdkpython:`, `sdkts:`, or `connector(<slug>):` subjects.

A Python or TypeScript package lists exactly the operations and Triggers it implements in `spec.languages.<language>`; the catalog and site show per-capability language support.

The first npm publish uses the scoped CI token; later publishes use trusted
publishing when configured. Keep token rotation and optional trust setup in
`docs/versioning-and-releases.md`.
