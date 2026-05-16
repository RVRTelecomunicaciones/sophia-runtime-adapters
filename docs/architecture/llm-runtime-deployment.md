# LLM-Capable Runtime Deployment — Design (Opción B)

**Status**: shipped (4 targets) — opencode 2026-05-15 (PR via ADR-0012) · ollama + aider + DRY base 2026-05-15 (PR #62)
**Driver**: cycle SDD real con LLM bloqueado en distroless puro (audit 2026-05-14)
**Companion**: opción A (binario local) ya validada y funcionando como workaround dev.

> **Nota de lectura (post-PR #62)**: este doc se conserva como design + decision log histórico. Para la **referencia operacional actual** de los 4 targets shipped (`runtime-adapters`, `-llm-opencode`, `-llm-ollama`, `-llm-aider`), ver el segundo apéndice al final del doc y `helm/sophia-runtime-adapters/values.yaml`. Para el **routing por phase** entre providers, ver `sophia-orchestator/docs/operations/llm-providers.md`.

## Problem statement

`runtime-adapters` corre en `gcr.io/distroless/static:nonroot` — base mínima sin shell ni binarios externos. Cuando `orch` solicita una capability `shell.exec@v1` con `command: "opencode"` (el dispatcher V1 default), el subprocess falla con `no such file or directory` porque distroless no incluye `opencode` ni ningún tooling externo.

**Consecuencia**: el cycle SDD real (spec → design → apply) muere en dispatch dentro de containers shipped. Sólo es ejecutable contra el binario local del host (opción A).

## Goals

- [ ] Permitir cycles SDD reales contra containers shipped (CI + staging + producción).
- [ ] Mantener distroless puro como default para deploys que NO necesitan LLM (e.g. capabilities-only, observability-only, etc.).
- [ ] Tamaño de imagen razonable (<300MB para el target con LLM).
- [ ] Update path claro para nuevas versiones de `opencode` (sin rebuild manual del Dockerfile).
- [ ] Preservar `nonroot` user, read-only filesystem-friendly, no shell unnecessary.

## Non-goals (out of scope)

- ~~Multi-LLM provider switch — eso es V2 (factory de adapters por agent_role).~~ **RESUELTO 2026-05-15**: V2 factory shipped en sophia-orchestator (PRs #18/#19/#20) + los 3 image targets en este repo (PR #62). Ver apéndice 2.
- Embedding del LLM en sí (modelos locales como Llama). Solo embedding del CLI client. **Nota**: el target `-llm-ollama` ships el `ollama` CLI, NO el daemon ni los modelos — conecta a un daemon externo via `OLLAMA_HOST`. La premisa de no embeber modelos sigue.
- Soporte cross-arch — empezamos con linux/arm64 + linux/amd64 build matrix. **Resuelto**: los 3 targets LLM se buildean para ambas arch (CI matrix en `.github/workflows/build-images.yaml`).

## Inventario de subprocesses que el runtime invoca

Auditando `RUNTIME_ALLOWED_COMMANDS_PATH` y los emit sites de `shell.exec@v1`:

| Subprocess | Frecuencia | Propósito |
|---|---|---|
| `opencode` | alta | dispatcher V1 — invoca al LLM |
| `git` | alta | apply phase (worktree, commits, diff, log) |
| `mkdir`, `rm`, `cp` | media | apply phase setup/teardown |
| `cat`, `head`, `wc` | baja | inspect output |

`git.worktree.create@v1` (PR #59 runtime) reemplaza al `git worktree` shell-out en muchos casos pero **no en todos** — `git commit`, `git push`, `git diff` siguen vía `shell.exec`.

**Implicación**: el container LLM-capable necesita **opencode + git + coreutils**, no solo `opencode`.

## Approach evaluado

### B.1 — alpine + apk

```dockerfile
FROM alpine:3.20 AS runtime-adapters-llm
RUN apk add --no-cache git curl openssh-client ca-certificates && \
    addgroup -g 65532 nonroot && adduser -u 65532 -G nonroot -D nonroot
COPY --from=opencode-bin /opencode /usr/local/bin/opencode
COPY --from=build /out/runtime-adapters /runtime-adapters
USER nonroot
ENTRYPOINT ["/runtime-adapters"]
```

| Pro | Con |
|---|---|
| Tamaño chico (~120MB) | musl libc puede romper binarios glibc-targeted |
| apk simple | opencode releases son glibc — necesita stage extra para extraer |
| Comunidad amplia | bash NO incluido por default |

### B.2 — distroless/cc

```dockerfile
FROM gcr.io/distroless/cc-debian12:nonroot AS runtime-adapters-llm
COPY --from=opencode-bin /opencode /usr/local/bin/opencode
COPY --from=git-stage /usr/bin/git /usr/bin/git
COPY --from=build /out/runtime-adapters /runtime-adapters
ENTRYPOINT ["/runtime-adapters"]
```

| Pro | Con |
|---|---|
| Más cerca del ideal distroless (~80MB) | git con todas sus deps requiere stage de extracción complejo |
| Sin shell, surface mínima | troubleshoot sin shell es doloroso |
| nonroot built-in | git puede tener deps dinámicas que rompen |

### B.3 — debian:12-slim (RECOMENDADO)

```dockerfile
FROM debian:12-slim AS runtime-adapters-llm
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl git openssh-client && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -g 65532 nonroot && useradd -u 65532 -g 65532 -m -s /sbin/nologin nonroot
COPY --from=opencode-bin /opencode /usr/local/bin/opencode
COPY --from=build /out/runtime-adapters /usr/local/bin/runtime-adapters
USER nonroot
ENTRYPOINT ["/usr/local/bin/runtime-adapters"]
```

| Pro | Con |
|---|---|
| Tamaño aceptable (~200MB) | Más grande que distroless |
| git con todas sus deps Just Works | No es tan minimal |
| apt para tooling adicional sin pain | |
| Compatible con cualquier binario glibc-linux | |
| Debian se trustea para producción más que alpine en muchos enterprises | |

## Decisión recomendada: B.3 (debian:12-slim)

**Razones**:
1. **git Just Works**: el apply phase necesita git — no vale la pena pelear con copy-paste de binarios.
2. **opencode glibc**: los releases upstream son glibc, alpine requiere fork.
3. **Tamaño aceptable**: 200MB es reasonable para un container que ejecuta agentes LLM. Comparado con images de Python/Node.js (~600MB+), es liviano.
4. **Troubleshoot**: `bash` está disponible para `docker exec` en debug urgente.
5. **Path claro a producción**: Debian es estable, predecible, supported by Distroless team upstream.

## Plan de implementación

### Fase 1 — Dockerfile target nuevo (~1h)

Agregar a `Dockerfile`:

```dockerfile
# ---- opencode-bin stage --------------------------------------------------
# Downloads opencode CLI from upstream release. Pinned by version + sha256.
ARG OPENCODE_VERSION=1.3.14
FROM debian:12-slim AS opencode-bin
ARG OPENCODE_VERSION
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && \
    curl -fsSL -o /tmp/opencode.tar.gz \
        "https://github.com/sst/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${TARGETARCH}.tar.gz" && \
    tar -xzf /tmp/opencode.tar.gz -C /tmp && \
    install -m 0755 /tmp/opencode /usr/local/bin/opencode

# ---- runtime-adapters-llm runtime stage ----------------------------------
# debian:12-slim base + opencode + git + ca-certs + nonroot user.
# Use this target when the runtime needs to spawn LLM dispatchers (cycle SDD).
# For pure capability execution without LLM, prefer the runtime-adapters target.
FROM debian:12-slim AS runtime-adapters-llm
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates git openssh-client && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -g 65532 nonroot && \
    useradd -u 65532 -g 65532 -m -s /sbin/nologin nonroot
COPY --from=opencode-bin /usr/local/bin/opencode /usr/local/bin/opencode
COPY --from=build /out/runtime-adapters /usr/local/bin/runtime-adapters
USER nonroot
ENTRYPOINT ["/usr/local/bin/runtime-adapters"]
```

### Fase 2 — Compose overlay (~30 min)

Nuevo file: `ops/local/compose.llm.yaml`

```yaml
# Overlay para usar runtime-adapters-llm en lugar del distroless default.
# Uso: docker compose -f compose.full-stack.yaml -f compose.llm.yaml up -d
services:
  runtime-adapters:
    build:
      context: ../../../sophia-runtime-adapters
      target: runtime-adapters-llm
    environment:
      RUNTIME_ALLOWED_COMMANDS_PATH: "/usr/local/bin:/usr/bin:/bin"
```

### Fase 3 — CI workflow (~30 min)

Nuevo job en `.github/workflows/ci.yaml` del runtime repo:

```yaml
build-llm-image:
  name: Build LLM-capable image
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: docker/setup-buildx-action@v3
    - name: Build runtime-adapters-llm
      uses: docker/build-push-action@v5
      with:
        context: .
        target: runtime-adapters-llm
        platforms: linux/amd64,linux/arm64
        tags: runtime-adapters-llm:test
```

### Fase 4 — Validation E2E (~30 min)

Nuevo test en `test/e2e/`:

```go
//go:build e2e_llm
// Cycles a real SDD phase against runtime-adapters-llm container,
// asserts the dispatcher invokes opencode and returns a parseable envelope.
```

### Fase 5 — Docs + ADR (~30 min)

ADR nuevo: `docs/adr/0007-runtime-llm-capable-target.md`

## Total estimado

**~3h** para implementación completa de B + tests + docs.

## Decisiones pendientes (de tu lado)

1. **OPENCODE_VERSION pinning**: ¿hardcoded en Dockerfile, parametrizable via `--build-arg`, o variable env del CI?
   - Recomendación: ARG con default a la versión actual (1.3.14), updateable via build-arg en CI matrix.

2. **Auto-update strategy**: ¿bot que abre PR cuando opencode tiene release nueva?
   - Recomendación: dependabot-style, PRs auto-generados weekly contra main.

3. **Multi-LLM previsto**: cuando llegue V2, ¿el container también incluye `claude-code`, `cursor`, `aider`?
   - Recomendación: targets separados (`runtime-adapters-claude-code`, `runtime-adapters-aider`, etc.) que extienden una base común. Evita single image con todos los CLIs (atrás de cada uno hay deps pesadas).
   - **Resuelto 2026-05-15 (PR #62)** — exactamente este approach: stage `llm-base` DRY + 3 LLM targets (`-llm-opencode`, `-llm-ollama`, `-llm-aider`). `claude-code` queda fuera mientras Anthropic siga bloqueando OAuth third-party (Feb 2026 ToS).

4. **Image registry**: ¿GHCR como las otras images? ¿Tag scheme (`runtime-adapters:1.0.0-llm`)?
   - Recomendación: GHCR con tag suffix `-llm` para distinguir del distroless default.

## Workaround mientras tanto (opción A — ya en uso)

Para iteración rápida en dev:

```bash
# 1. Stop el container
docker stop sophia-runtime-adapters

# 2. Reconfigure orch para apuntar al host
docker stop sophia-orchestator && docker rm sophia-orchestator
# (re-run con SOPHIA_RUNTIME_URL=http://host.docker.internal:8083)

# 3. Run runtime-adapters local
RUNTIME_HTTP_ADDR=":8083" \
RUNTIME_POSTGRES_DSN="postgres://runtime:runtime@localhost:5437/runtime_adapters?sslmode=disable" \
RUNTIME_VERSION="0.1.0-local" \
RUNTIME_ALLOWED_COMMANDS_PATH="$HOME/.opencode/bin:/usr/local/bin:/usr/bin:/bin" \
RUNTIME_ALLOWED_WORKING_DIRS="/tmp:$HOME/Documents/2026" \
RUNTIME_ALLOWED_FILESYSTEM_ROOTS="/tmp:$HOME/Documents/2026" \
RUNTIME_ENV="development" \
PATH="$HOME/.opencode/bin:$PATH" \
go run ./cmd/runtime-adapters
```

Validado el 2026-05-15: `execution complete | duration_ms=4277 | status=success` (LLM real respondiendo).

## Trade-offs honestos

> Esta tabla refleja la situación **post-PR #62** (4 targets shipped). La versión original de la tabla (2 columnas) está conservada en el historial de git para referencia.

| Tema | `runtime-adapters` (distroless) | `-llm-opencode` | `-llm-ollama` | `-llm-aider` |
|---|---|---|---|---|
| Base | distroless/static | debian:12-slim + tini | debian:12-slim + tini | debian:12-slim + tini + python3 |
| Tamaño aprox. | ~30MB | ~520MB | ~80MB (CLI sin daemon) | ~500MB (pipx venv) |
| Surface attack | mínima | git + opencode + curl | git + ollama CLI | git + python3 + aider venv |
| LLM cycles | ❌ no funcionan | ✅ vía OAuth en auth.json | ✅ contra `OLLAMA_HOST` externo | ✅ vía API keys env (ANTHROPIC_API_KEY etc.) |
| Update CLI | N/A | rebuild + bump `OPENCODE_VERSION` | rebuild + bump `OLLAMA_VERSION` | rebuild + bump aider via pipx pin |
| Producción | ✅ shipped | ✅ shipped | ✅ shipped (necesita daemon externo) | ✅ shipped (necesita secrets de provider) |

**Strategy**: los 4 targets coexisten. `runtime-adapters` para deploys puramente reactivos. Los 3 `-llm-*` se eligen según qué adapter tiene que invocar `shell.exec@v1` el orchestator (cf. `SOPHIA_DISPATCHER_PROVIDER_*` en orchestator). El operador NO mezcla varios CLIs en una sola imagen — cada deployment elige UN target LLM y configura `helm.values.llm.target = opencode|ollama|aider` para que el chart monte sólo lo correspondiente.

## Próximo paso

Cuando quieras arrancar B, este doc es la guía. ~3h de trabajo, fase por fase. Recomiendo arrancar con Fase 1 + 2 + validar manualmente antes de Fase 3 (CI) y 4 (test E2E).

---

## Apéndice — Implementación final (2026-05-15)

Opción B fue implementada y validada el mismo día. Ver **ADR-0012** para la decisión completa, las opciones consideradas y las consecuencias.

**Trabajo entregado**:
- `Dockerfile`: stages `opencode-bin` + `runtime-adapters-llm` agregados (debian:12-slim base + opencode pinned + git + tini + nonroot)
- `ops/local/compose.llm.yaml`: overlay para activar el target
- `ops/local/opencode-config.json`: permissions `allow` (mitiga bug upstream `#14473`)
- `.github/workflows/ci.yaml`: nuevo job `build-llm-image` (linux/amd64 + linux/arm64 via buildx + GHA cache)
- `docs/adr/0012-runtime-llm-capable-target.md`: ADR aceptado

**Validación end-to-end** (cycle SDD real contra container LLM, NO host):
- Phase `01KRNRSXA99KT957QN9XHNVAX3` → events: `phase.started` → `governance.decision` → `agent.dispatched` → `agent.envelope.received` → `phase.completed` (status DONE)
- `execution complete | duration_ms=9222 | status=success` dentro del container
- Provider: `github-copilot/claude-sonnet-4.6` via OAuth (auth.json bind-mounted como secret)

**Findings concretos durante la implementación** (gotchas que el spec original no anticipó):

| Gotcha | Fix aplicado |
|---|---|
| Asset `opencode-linux-arm64.tar.gz` SOLO existe hasta v1.14.48 (v1.14.49+ removieron tarballs CLI, dejan solo desktop) | Pin a `OPENCODE_VERSION=1.14.48` |
| `ARG OPENCODE_VERSION` global antes del primer `FROM` NO se hereda en stages posteriores para uso en `RUN` | Re-declarar `ARG OPENCODE_VERSION=1.14.48` con default explícito dentro del stage |
| `tar -xzf opencode.tar.gz -C /tmp` deja `/tmp/opencode` como FILE, lo que rompe `opencode --version` (intenta crear `/tmp/opencode` como directory para runtime files) | Extract a `/tmp/opencode-dl/` aislado, `install` al `/usr/local/bin/`, después `rm -rf` el dir |
| `docker run --name sophia-runtime-adapters` NO crea DNS alias `runtime-adapters` automáticamente | Agregar `--network-alias runtime-adapters` (compose lo hace transparente, docker run no) |
| Tamaño final real **519 MB** (vs 200 MB estimado) — bun-compiled opencode pesa ~120 MB descomprimido | Documentado en ADR como trade-off aceptado |
| OAuth credentials viven en `~/.local/share/opencode/auth.json` del HOST | Bind-mount como secret read-only en compose overlay |
| Anthropic bloquea server-side el OAuth Claude Code de third-party tools | Usar `github-copilot/claude-sonnet-4.6` (Microsoft tiene deal con Anthropic para Copilot) |


---

## Apéndice 2 — Multi-LLM expansion (2026-05-15, PR #62)

La Fase 1 (apéndice anterior) entregó `runtime-adapters-llm` con `opencode` bundled. PR #62 amplió esa base a **4 image targets** con stage común DRY, manteniendo el contrato del operador (`shell.exec@v1` + `RUNTIME_ALLOWED_COMMANDS_PATH`) intacto.

### Targets shipped

| Tag suffix | Base | Bundled CLIs | Cuándo usar |
|---|---|---|---|
| _(none)_ — `runtime-adapters` | distroless/static | — | Deploys reactivos sin LLM (capabilities-only). |
| `-llm-opencode` | `llm-base` (debian:12-slim + tini + git) | `opencode` 1.14.48 + auth.json bind-mount | Default LLM target — orchestator usa opencode como provider por defecto. |
| `-llm-ollama` | `llm-base` | `ollama` CLI (no daemon) | Cuando `SOPHIA_DISPATCHER_PROVIDER_*=ollama` está activo. La imagen NO trae daemon — conecta a uno externo via `OLLAMA_HOST` env (default chart: `http://ollama.ollama.svc.cluster.local:11434`). |
| `-llm-aider` | `llm-base` + `python3` | aider via pipx venv | Cuando `SOPHIA_DISPATCHER_PROVIDER_APPLY=aider` está activo. Las creds (`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`) viven en `helm.values.secrets.aiderEnv`, no en auth.json. |

Para backward-compat el tag `-llm` sin suffix se mantiene como alias de `-llm-opencode`.

### Helm migration: `llm.enabled` → `llm.target`

Values schema cambió de toggle booleano a enum:

```yaml
# Antes (Fase 1):
llm:
  enabled: true   # mountaba opencode auth.json y nada más

# Después (PR #62):
llm:
  target: opencode | ollama | aider | ""   # "" desactiva mounts LLM
```

El chart consulta `llm.target` para decidir qué montar:
- `opencode` → monta `auth.json` Secret (igual que antes).
- `ollama` → expone `OLLAMA_HOST` env, NO monta auth.json ni aiderEnv.
- `aider` → monta `secrets.aiderEnv` Secret (`ANTHROPIC_API_KEY` etc.), NO monta auth.json ni `OLLAMA_HOST`.
- `""` (vacío) → ningún mount LLM (equivalente al antiguo `llm.enabled: false`).

Migration path para deploys existentes con `llm.enabled: true`:
```bash
helm upgrade ... --set llm.target=opencode --set llm.enabled=null
```

### CI matrix

`.github/workflows/build-images.yaml` ahora buildea **3 targets × 2 platforms = 6 jobs en matriz**:

```yaml
strategy:
  matrix:
    target:   [runtime-adapters-llm-opencode, runtime-adapters-llm-ollama, runtime-adapters-llm-aider]
    platform: [linux/amd64, linux/arm64]
```

Push a `ghcr.io/rvrtelecomunicaciones/sophia-runtime-adapters:<version>-<target-suffix>` solo en main; en PRs se buildea sin push para validar.

### Findings adicionales (más allá del apéndice 1)

| Gotcha | Fix aplicado |
|---|---|
| `ARG OLLAMA_VERSION=0.9.0` global antes del primer `FROM` no fluye al re-declarar dentro del stage `ollama-bin` (mismo bug que `OPENCODE_VERSION` en Fase 1) | Re-declarar con default explícito DENTRO del stage. Patrón `ARG`-en-stage replicado para los 2 nuevos targets. |
| El tarball de ollama tiene layout `bin/ollama` (no `ollama` en root) | Extract a `/tmp/ollama-dl/` y copy desde `/tmp/ollama-dl/bin/ollama` |
| pipx venv de aider pesa ~350 MB (full Python dep tree de aider-chat) | Aceptado en el ADR (~500 MB final, comparable al opencode target) |
| Aider necesita `python3` (`libpython3.11.so`) en el runtime image, pero NO `pip` ni `pipx` | El stage `aider-bin` corre pipx en build-time, copia la venv self-contained al runtime image. Runtime no tiene pipx. |
| Validación local: `ollama run` en la imagen reporta "could not connect to a running Ollama instance" | Esperado — la imagen NO trae daemon. El operator debe correr `ollama serve` externo o usar uno managed. |

### Ver también

- `helm/sophia-runtime-adapters/values.yaml` — nuevo schema `llm.target`
- `Dockerfile` — stages `llm-base` + `ollama-bin` + `aider-bin` + 4 targets finales
- `.github/workflows/build-images.yaml` — CI matrix
- `sophia-orchestator/docs/operations/llm-providers.md` — operator-facing routing guide para los 3 providers
