# Serial account model tests

## Workflow

1. Open an account's connection test and select **Serial model tests**.
2. Open **Configure model list**. Enter explicit model IDs, one per line, in the
   desired order. The saved preset is shared by accounts on the same platform.
3. Start the test. Each probe finishes before the next begins. Failures do not
   stop the batch. Each model has a 120-second timeout; batches allow 1-30 models.
4. Inspect availability, upstream model, duration, errors and the test timestamp.
   The latest batch replaces the previous batch and is persisted after every
   result. Stop/closing the dialog cancels the request; remaining models are not
   tested. A lost connection or process restart may leave a partial run marked
   running; the UI labels it as unfinished when reloaded.
5. In **Edit account**, expand **Remap from latest test**, explicitly choose a
   successful target, and review/edit the source model list. Failed probes are
   suggested as sources. Apply the mapping draft, then save the account normally.

No mapping is automatically changed by a test. Existing unrelated mappings are
preserved. The chosen target receives an identity mapping to avoid an existing
rule redirecting it elsewhere. Applying an OpenAI mapping turns off account
passthrough in the draft, since passthrough would bypass these mappings.

## Semantics and safety

- Batch probes bypass configured account model mappings. OpenAI Codex probes
  also bypass model alias normalization so unknown models cannot silently test
  a different default model. Existing single-model tests are unchanged.
- These are real default-mode connection tests, not a model-list lookup. They
  can incur provider usage and retain the existing probe's account-state effects.
- Success is a point-in-time result for the tested request, not a guarantee of
  future availability, full feature compatibility, quota or context capacity.
  A temporary 429/503 is a failed probe, not proof that a model is unsupported.
- Remapping does not override group permissions or gateway model filtering.
  Mapping text, image and other incompatible capabilities is not a substitute
  for supporting those capabilities.
- One batch per account is permitted within a service process. Multi-replica
  deployments would require a distributed lock to enforce that across replicas.
- A stream must include a successful completion event to count as success.
  Browser disconnects cancel probes but completed results are still persisted.
- Ordinary account edits preserve the latest database result under the existing
  account row lock. Account duplication discards the source's test result.

## Storage and API

No schema migration is required. Platform presets use the existing `settings`
store with keys `account_serial_test_models_<platform>`. Results use the account
`extra.latest_serial_model_test` field and are returned by the admin account API.

All new endpoints use the existing administrator middleware:

- `GET /api/v1/admin/accounts/:id/test-models/preset`
- `PUT /api/v1/admin/accounts/:id/test-models/preset`, body `{"models":["gpt-6-astra","gpt-5.4"]}`
- `POST /api/v1/admin/accounts/:id/test-models`, same body; SSE events are
  `snapshot`, `heartbeat`, `complete`, and `error`. Only `complete` confirms the
  final run has been saved. A conflicting batch returns HTTP 409.

Build and deploy the backend and frontend together to expose the new endpoints
and controls. Implementing this feature does not require restarting databases.
