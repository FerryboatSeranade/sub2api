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
