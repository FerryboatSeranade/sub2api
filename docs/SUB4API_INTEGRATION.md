# hz Sub4API integration

Upstream pinned to huoji120/sub4api a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e; base production 191da77ed.

Keeps the existing module identity, platform catalog, billing security fixes, vm2api bridge and dedicated per-account keys, reasoning exclusion policy, scheduled probes, pelican/Astra selection, administrator usage account column, and blue/green tooling.

Adds upstream Codex HTTP/WebSocket protocol refinements, ticket lifecycle/diagnostics, BPS provider and opt-in request audit. BPS requires explicit composite routing. Existing client model metadata and search replay are retained.

Compatibility resolutions: migrations 242 and 244 preserve existing timezone and harvesting settings; 245/247 do not reintroduce fixed platform constraints; 249 refuses to discard nonempty audit history. Incoming background harvest and auto-diagnostic loops honor the deployment owner marker. Request auditing and automatic diagnostics remain opt-in. Existing release/CI workflows and production version branding are retained; upstream's image publishing workflow is not enabled.

Rollback uses the prior immutable v9 image and smooth nginx reload. Newly added schema is retained on rollback; never restore a stale database dump over live traffic. Database clone migration and old-version-against-upgraded-schema verification are required before release.
