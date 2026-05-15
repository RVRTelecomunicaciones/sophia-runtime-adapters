# ADR 0012 — `runtime-adapters-llm` Dockerfile target

- **Status:** accepted
- **Date:** 2026-05-15
- **Deciders:** Russell Vergara
- **Context:**
  The default `runtime-adapters` runtime image is `gcr.io/distroless/static:nonroot` — a
  ~30 MB base with no shell, no `/bin`, no package manager. That surface is ideal for the
  pure-execution layer per D1.4 (governable execution boundary), but it cannot host any
  external CLI subprocess.

  The orchestrator's V1 dispatcher invokes the LLM via `shell.exec@v1` with `command: "opencode"`.
  Against the distroless image the subprocess fails immediately with `no such file or directory`
  because `opencode` does not exist inside the container. End-to-end SDD cycles with a real LLM
  therefore cannot run against the shipped runtime container — only against a binary launched
  on the host (workaround "opción A", documented in `docs/architecture/llm-runtime-deployment.md`).

  We need a sibling target that bundles the `opencode` CLI plus the minimal tooling Sophia's
  apply-phase agents touch (git, ssh, ca-certs) so cycles SDD reales can execute fully inside
  the container — for CI, staging, and production.

- **Options considered:**
  - **A. Alpine base + apk** — Smallest footprint (~120 MB) but `opencode` upstream tarballs are
    glibc-targeted; the `-musl` variants exist but have less coverage. Reject: musl/glibc fragility
    on a binary we don't control.
  - **B. Distroless/cc + manual COPY of git** — Closest to the existing distroless surface
    (~80 MB) but git carries dynamic deps that require a complex extract stage; troubleshooting
    in production without a shell is painful. Reject: maintenance cost outweighs the size win.
  - **C. debian:12-slim + opencode tarball + apt-get tooling** *(chosen)* — ~520 MB but every
    layer is one of: an officially-supported Debian package, the upstream opencode binary, the
    Go runtime-adapters binary. Predictable, debuggable, supported.
- **Decision:**
  Add a `runtime-adapters-llm` target to `Dockerfile` based on `debian:12-slim`. It coexists
  with the existing distroless `runtime-adapters` target; operators pick by build-arg target.
  The accompanying compose overlay (`ops/local/compose.llm.yaml`) wires the LLM target plus
  the read-only mounts required for headless operation.

- **Consequences:**
  - **Two runtime images, one source binary.** The Go binary built by the shared `build` stage is
    identical between `runtime-adapters` and `runtime-adapters-llm`; the runtime image is the only
    difference. The Phase 1 contract (R3, R5, R13) is unchanged.
  - **Image size jumps to ~520 MB** for the LLM target (vs ~30 MB distroless). Trade-off
    rationale: the bun-compiled `opencode` standalone binary alone is ~120 MB extracted, plus
    debian-slim base (~80 MB) plus git+ssh+libs (~50 MB) plus apt overhead. Acceptable for a
    target that exists specifically to host LLM dispatcher subprocesses.
  - **`opencode` version is pinned** via `ARG OPENCODE_VERSION=1.14.48`. Override per-build with
    `--build-arg OPENCODE_VERSION=...`. v1.14.48 is the last release shipping the standalone CLI
    tarball; v1.14.49+ removed the tarball assets in favor of desktop-only `.deb` / `.AppImage`
    distribution. Bumping past 1.14.48 will require either revisiting the install method or
    waiting for upstream to restore CLI tarballs.
  - **No upstream checksum verification.** GitHub releases for `anomalyco/opencode` do not publish
    `.sha256` / `.sig` files (verified 2026-05-15 via `gh release view --json assets`). The build
    trusts HTTPS to the GitHub CDN. If supply-chain integrity becomes a hard requirement, we
    maintain a checksum map in-repo and verify it inline; documented as deferred work.
  - **`tini` is PID 1** in the LLM target. Required to mitigate upstream bug
    `anomalyco/opencode#17516` (`opencode run` may not exit cleanly after tool-use returns when
    the model is `github-copilot/claude-sonnet-4.6`). Tini reaps zombies and forwards signals;
    combined with `shell.exec@v1`'s enforced `timeout_budget_ms` this bounds the worst-case impact.
  - **Permissions config is mounted as a secret.** The `ops/local/opencode-config.json` file
    pre-grants `bash`/`edit`/`write`/`read`/`external_directory` to `allow`. Without it, opencode
    defaults to `ask` and hangs forever in headless mode (upstream bug `#14473`, closed but the
    default unchanged).
  - **OAuth credentials are operator-supplied.** The container ships with no credentials; operators
    bind-mount `~/.local/share/opencode/auth.json` (the OAuth token store) read-only at
    `/home/nonroot/.local/share/opencode/auth.json`, OR pass provider API keys via env (e.g.
    `ANTHROPIC_API_KEY`). For Sophia local-stack the recommended pairing is GitHub Copilot OAuth
    + `SOPHIA_DISPATCHER_MODEL=github-copilot/claude-sonnet-4.6`, which avoids the
    Anthropic-third-party-block (Anthropic actively rejects OAuth tokens originating from
    non-official Claude Code clients, returning "out of usage" even when the subscription has
    quota).
  - **CI gains a `build-llm-image` job** in `.github/workflows/ci.yaml` building both
    `linux/amd64` and `linux/arm64` via buildx with GHA cache. It is independent of the
    `lint-unit-contract` job (image build is not gated by Go tests; both surfaces run in
    parallel).
  - **`docs/architecture/llm-runtime-deployment.md` is the operator-facing companion** — it
    documents both opción A (host binary, dev workaround) and opción B (this target).

- **Spec references:** D1.1 (governable boundary intact); D1.4 (runtime image as the
  reproducibility surface); D1.6 (Phase 1 capabilities unchanged — only the dispatcher's
  ability to spawn opencode is enabled); R3 (port contracts frozen); R5 (raw outcome stays in
  adapter — opencode subprocess output flows through `ResultNormalizer`); R13 (every execution
  produces an `ExecutionReceipt` regardless of dispatcher path).
