# Test tenant setup — Phase 2C.4 / A+B

> **Audience:** operator setting up a development environment to
> run `make smoke-receivers` (or local-dev compose with real
> receivers) end-to-end.
>
> **NOT for CI.** CI runs against mocked receivers + a Postgres
> testcontainer. Test-tenant credentials are loaded only when an
> operator explicitly drives the smoke target locally.

## 1. Why a test tenant

- Production credentials NEVER reach a developer machine. The
  4-layer CI guardrail (gitleaks + env-var-guard + bootstrap mode
  lock + tenant fingerprinting) enforces this on every PR; the
  test tenant policy enforces it during local development.
- The smoke target hits real PagerDuty / Slack / Linear APIs. With
  a prod tenant a wrong env var would page a real on-call rotation
  or spam a customer-facing channel. With a test tenant the same
  mistake creates a bench incident on a service NOBODY watches.

## 2. PagerDuty

### 2.1 Account

- Free tier supports 5 services — sufficient for our test service.
- Operator's personal PagerDuty account is acceptable; a shared
  team test account is preferable but optional.

### 2.2 Test service

In the PagerDuty UI:

1. Services → New Service.
2. Name: `runtime-adapters-test` (or `<your-username>-runtime-test`).
3. **Escalation Policy:** assign to a policy that has NO real human
   on-call. Either a "do nothing" policy that escalates to nobody
   AFTER several hours, or a policy whose first level is the
   operator's own PagerDuty user. The smoke target resolves the
   incident immediately during cleanup; pages should NEVER actually
   wake a human.
4. **Integration:** add an integration of type `Events API v2`.
5. Capture:
   - **Integration / routing key** (32-char alphanumeric)
     → `PAGERDUTY_TEST_ROUTING_KEY` (used by alertmanager.yaml)
   - **Service ID** (from the service URL, e.g.,
     `https://your.pagerduty.com/service-directory/PXXXXXX`)
     → `PAGERDUTY_TEST_SERVICE_ID` (used by smoke verify)

### 2.3 API token

PagerDuty UI → User Profile → User Settings → Create API User Token.

- Scope: `read` is sufficient (smoke calls /incidents to query, and
  PUT /incidents/<id> to resolve — both work with a personal user
  token; service-scoped read tokens are not granular enough for
  this use case).
- → `PAGERDUTY_TEST_API_TOKEN`

## 3. Slack

### 3.1 Workspace

A dedicated sandbox workspace is preferred. If the team has only
one workspace, use 2 dedicated test channels (e.g.,
`#alerts-test-incidents` + `#alerts-test-ops`) and scope the bot
user STRICTLY to those channels.

### 3.2 Channels

Create 2 channels:

- `#alerts-test-incidents` — receives critical alerts via
  Alertmanager `slack_configs[].api_url`.
- `#alerts-test-ops` — receives warning alerts.

Capture each channel's **channel ID** (Slack UI → channel details →
About → channel ID at the bottom):
- `SLACK_TEST_INCIDENTS_CHANNEL_ID`
- `SLACK_TEST_OPS_CHANNEL_ID`

### 3.3 Incoming webhooks (Alertmanager → Slack)

For each channel, add an **Incoming Webhook** integration:

1. Slack UI → Apps → search "Incoming Webhooks" → install.
2. Choose the test channel.
3. Copy the webhook URL (https://hooks.slack.com/services/T.../B.../...).
4. → `SLACK_TEST_INCIDENTS_WEBHOOK_URL` and
     `SLACK_TEST_OPS_WEBHOOK_URL` respectively.

### 3.4 Bot user (smoke verification + cleanup)

The smoke target reads channel history + deletes its own test
messages. This requires a bot user (NOT a personal token).

1. Slack API → Your Apps → Create New App → From Scratch.
2. Name: `runtime-adapters-smoke-bot`. Workspace: your test workspace.
3. OAuth & Permissions → Bot Token Scopes:
   - `channels:history` — read messages in test channels (smoke verify).
   - `chat:write` — delete the bot's own messages (smoke cleanup).
4. Install to Workspace.
5. Invite the bot to BOTH test channels:
   `/invite @runtime-adapters-smoke-bot` from inside each channel.
6. Copy the **Bot User OAuth Token** (starts with `xoxb-`).
7. → `SLACK_TEST_BOT_TOKEN`

> **Important:** the bot's `channels:history` scope is restricted to
> channels it has been explicitly invited to. Do NOT grant
> workspace-wide history access — Layer 4 fingerprinting (best-effort)
> assumes the bot can only see test channels.

## 4. Linear

### 4.1 Workspace

Either a dedicated test workspace OR a `test` project label inside
the production workspace. The free tier supports unlimited issues
in a single workspace; a separate workspace is cleaner.

### 4.2 Team

Create a Team named `Smoke Test` (or similar):

- Settings → Teams → New Team.
- Capture the team ID via GraphQL:
  ```graphql
  { teams { nodes { id name } } }
  ```
  Run via `https://studio.apollographql.com/public/Linear-API` or
  curl with your API token.
- → `LINEAR_TEST_TEAM_ID`

### 4.3 Pre-created labels

Create these labels in the test team:

- `alert-managed` — applied to every adapter-created issue
  (D2C4AB.7).
- `severity:critical` — applied to critical-severity issues.
- `severity:warning` — applied to warning-severity issues.

The adapter creates the per-grouping `alert:<hash>` labels on
demand at issue creation time — no manual setup needed.

### 4.4 API token

Linear Settings → API → Personal API Keys → Create.

- Scope: `Admin` on the test team is sufficient. The token is
  per-user; revoke + rotate before sharing the workspace with
  others.
- → `LINEAR_TEST_API_TOKEN`

## 5. Env var summary

After completing sections 2-4, the operator's `.env` (copied from
`.env.example`) should look like:

```bash
# B1 — Alertmanager native receivers
PAGERDUTY_TEST_ROUTING_KEY=R0XXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
SLACK_TEST_INCIDENTS_WEBHOOK_URL=https://hooks.slack.com/services/T0.../B0.../...
SLACK_TEST_OPS_WEBHOOK_URL=https://hooks.slack.com/services/T0.../B0.../...

# B2 — Linear webhook adapter
LINEAR_TEST_API_TOKEN=lin_api_XXXX...
LINEAR_TEST_TEAM_ID=00000000-0000-0000-0000-000000000000
LINEAR_TENANT_TYPE=test
RUNTIME_TENANT=test
LISTEN_ADDR=:9095

# B3 — Smoke target
PAGERDUTY_TEST_API_TOKEN=u+XXXX...
PAGERDUTY_TEST_SERVICE_ID=PXXXXXX
SLACK_TEST_BOT_TOKEN=xoxb-...
SLACK_TEST_INCIDENTS_CHANNEL_ID=CXXXXXXXXXX
SLACK_TEST_OPS_CHANNEL_ID=CXXXXXXXXXX
```

## 6. Running the smoke

```bash
# 1) Source the env
set -a; . .env; set +a

# 2) Bring up the receivers compose profile
docker compose -f ops/local/compose.yaml --profile receivers up -d

# 3) Wait for /healthz on alertmanager + linear-webhook
curl -sf http://localhost:9093/-/ready
curl -sf http://localhost:9095/healthz

# 4) Run the smoke
make smoke-receivers
```

Expected: ~2 minutes (90s wait + ~30s API calls). Final log line
`RESULT: PASS`. Exit code 0.

If it fails:
- Read the dumped alertmanager + linear-webhook logs.
- Verify the env vars were sourced (`echo $LINEAR_TEST_TEAM_ID`).
- Verify amtool can reach the alertmanager:
  `amtool --alertmanager.url=http://localhost:9093 alert query`.

## 7. Cleanup between runs

The smoke's cleanup phase resolves the PagerDuty incident, deletes
the Slack messages, archives the Linear issue, and silences the
SmokeTest* alertnames for 5 minutes. Cleanup is fail-soft (D2C4AB.14)
— individual step failures log warnings but don't abort.

If cleanup fails, manually:

- PagerDuty UI → resolve the SmokeTest incident.
- Slack UI → delete the SmokeTest message manually if `chat.delete`
  failed (Slack permits message authors to delete; the bot user is
  the author of these messages).
- Linear UI → archive the SmokeTest issue.

## 8. Rotation policy

Rotate every credential at least once per quarter. Rotation is the
operator's responsibility — there is no automated rotation in
v0.8.0.

If a credential is suspected leaked: revoke immediately at the
provider, rotate the new value, update `.env`, and run `gitleaks`
locally on the repo (`docker run --rm -v "$(pwd):/repo"
zricethezav/gitleaks:latest detect --source /repo`) to confirm no
historical commit contains the old value.
