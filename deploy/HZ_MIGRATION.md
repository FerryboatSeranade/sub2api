# hz deployment

Production checkout: `/root/data/docker_data/sub2api-dev` on hz.
Development branch: `dev/secondary-development-20260525`.
Upstream v0.2.17 is merged with the existing OVH development history.

```sh
cd /root/data/docker_data/sub2api-dev/deploy
docker compose -f docker-compose.hz.json up -d
docker compose -f docker-compose.hz.json ps
```

The application listens on port 8081. PostgreSQL and Redis are only on the
Compose network. Runtime secrets are stored in ignored `.hz-private/*.env`
files with restricted permissions. These files must accompany a deployment
but must never be committed. Persistent data is under `deploy/staging/`.
The Compose default uses the image `sub2api-staging:hz-20261017` built locally
from this branch, rather than pulling an unrelated public image.

Migration snapshots and logs are outside this checkout at
`/root/data/docker_data/sub2api-migration-20261017`.
The original OVH checkout, database and application container are retained.
Do not start both applications against divergent databases after cutover.
Rollback after new writes requires transferring those writes back first.

## vm2api upstream

The `vm2api-hz` OpenAI group uses the account with the same name (concurrency
2), advertising `gpt-6-sol` and `gpt-6-luna`. Select this group when creating a
Sub2API client API key. The normal client base URL remains
`https://code.gogoais.com/v1`.

`vm2api-relay` exposes only `/v1/` on the private Docker bridge gateway
`172.20.0.1:8788`, forwarding to the host-local vm2api listener
`127.0.0.1:8787`. It does not publish the vm2api management panel. Both services
restart automatically. If the Docker network is deliberately deleted and
recreated with a different subnet, update the relay listener and the account's
base URL together. The vm2api client key remains in private runtime storage
and Sub2API credentials, never in Git.
