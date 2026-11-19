# Public API Key usage lookup

No dashboard login is required. Supply the **complete** API key in
`Authorization: Bearer <key>` or `x-api-key: <key>`. Partial keys are not supported.
Never put a key in URL query parameters. Use HTTPS.

## Records

`GET /v1/usage/records`

Parameters:

| Parameter | Meaning | Default |
| --- | --- | --- |
| model | Exact client-requested model name; omit for all models | all |
| start_time | Inclusive RFC3339 timestamp with timezone | 7 days before end_time |
| end_time | Exclusive RFC3339 timestamp with timezone | now |
| page | Page number, 1–10000 | 1 |
| page_size | Records per page, 1–100 | 20 |

A query can span at most 31 days. Use multiple adjacent intervals for older history.
Records are sorted newest first. Concurrent requests may add new records; use a fixed
end_time when paging. The endpoint returns persisted usage logs, not an error log or
request-body archive. Recently completed requests may appear after asynchronous usage
settlement. Historical records remain subject to the site's retention policy.

```sh
curl 'https://code.gogoais.com/v1/usage/records?model=gpt-6.1-sol&page=1&page_size=20' \
  -H "Authorization: Bearer $SUB2API_KEY"
```

Response uses the standard `{code, data}` envelope, with
`data.items`, `data.total`, `data.page`, `data.page_size`, and `data.pages`.
Each item contains `id`, `request_id`, `model`, `input_tokens`, `output_tokens`,
`cache_creation_tokens`, `cache_read_tokens`, `total_cost`, `actual_cost`,
`duration_ms`, `stream`, and `created_at`.
`actual_cost` is the recorded user charge; `total_cost` is the recorded cost before
the user multiplier. Neither is an upstream account cost. Upstream account identities,
credentials, user IDs, IP addresses and request bodies are not returned.

## Model choices

`GET /v1/usage/models`

Accepts the same `start_time` and `end_time`. Returns
`data.items: [{model, requests}]` for models actually used by this key in the interval.
The `model` filter is ignored here so a dropdown can show all choices. These are
historical model names, not the provider's current supported-model catalog.

```sh
curl 'https://code.gogoais.com/v1/usage/models' \
  -H "Authorization: Bearer $SUB2API_KEY"
```

Both endpoints enforce the authenticated key and its owner. Parameters `api_key_id`,
`user_id`, `account_id`, `group_id`, and `model_source` are rejected. Expired or quota-
exhausted keys may read their history; disabled/deleted keys, disabled users, IP
restrictions and existing group access controls remain enforced. Queries do not consume
model quota or update key last-used timestamps. Responses use `Cache-Control: no-store`.
Invalid filters return 400; absent/invalid credentials return 401; access restrictions
return 403. Each database query has a 15-second deadline.
