# sophia-runtime-adapters Helm chart

Effects layer for the Sophia ecosystem. Two image targets are supported:

| Target | Base | Use case |
|--------|------|----------|
| `runtime-adapters` (default) | `gcr.io/distroless/static:nonroot` | Pure capability execution, no LLM subprocess |
| `runtime-adapters-llm` | `debian:12-slim` + opencode + git + tini | SDD cycles invoking LLMs via shell.exec@v1 |

See `docs/adr/0012-runtime-llm-capable-target.md` for the design.

## Quick install — distroless target (default)

```bash
helm install runtime ./helm/sophia-runtime-adapters \
  --set image.tag=0.1.0 \
  --set secrets.postgresDsn="$(vault kv get -field=dsn secret/sophia/runtime)"
```

## Install — LLM-capable target

```bash
# 1. Encode your opencode auth.json (OR use External Secrets Operator)
AUTH_B64=$(base64 < ~/.local/share/opencode/auth.json | tr -d '\n')

# 2. Install with llm.enabled + the LLM image tag
helm install runtime ./helm/sophia-runtime-adapters \
  --set image.tag=0.1.0-llm \
  --set llm.enabled=true \
  --set openCodeAuth.authJsonBase64="$AUTH_B64" \
  --set secrets.postgresDsn="$(vault kv get -field=dsn secret/sophia/runtime)"
```

When `llm.enabled=true`:
- A ConfigMap with `opencode.json` (permissions=allow) is mounted at `/home/nonroot/.config/opencode/opencode.json`.
- A Secret with `auth.json` is mounted at `/home/nonroot/.local/share/opencode/auth.json` (only if `openCodeAuth.authJsonBase64` is provided).
- Writable `emptyDir` volumes for `/tmp` and `~/.local/share/opencode` (opencode + bun runtime files).
- `readOnlyRootFilesystem` is auto-flipped to `false` (opencode needs writable HOME).

## Production checklist

- [ ] Secrets via External Secrets Operator (Vault/AWS SM/GCP SM) — NEVER commit `authJsonBase64` to values.yaml
- [ ] `image.tag` pinned to a digest, not a mutable tag
- [ ] `secrets.postgresDsn` uses `sslmode=require`
- [ ] `config.env=production` (NOT `development` — disables chaos hooks)
- [ ] `config.chaosEnabled=false` in any non-chaos-test environment
- [ ] NetworkPolicy: only the `sophia-orchestator` Pod selector reaches `:8080`
- [ ] When `llm.enabled=true`, audit the OAuth providers in auth.json — only what the dispatcher needs
- [ ] For multi-arch clusters, build with `docker buildx build --platform linux/amd64,linux/arm64`

## Probes

- Liveness: `GET /healthz` (process aliveness)
- Readiness: `GET /readyz` (DB connectivity check)

## Resources

LLM target (debian-slim + opencode bun runtime) needs more memory than the
distroless default. Defaults are conservative; tune based on observed
RPS + LLM subprocess concurrency (see `RUNTIME_HTTP_ADDR` rate-limit
headers and prometheus metrics).

## See also

- `sophia-runtime-adapters/Dockerfile` — multi-target build
- `sophia-runtime-adapters/docs/adr/0012-runtime-llm-capable-target.md`
- `sophia-runtime-adapters/docs/architecture/llm-runtime-deployment.md`
- `sophia-orchestator/docs/operations/llm-providers.md` — operator-facing matrix
