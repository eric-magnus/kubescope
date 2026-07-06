# Kubescope — LaunchDarkly SE Technical Exercise

Kubescope is a mock Kubernetes vulnerability/runtime-findings dashboard, built to exercise
LaunchDarkly feature flags, targeting, experimentation, and AI Configs. Written in Go, using the
[LaunchDarkly Go server-side SDK](https://docs.launchdarkly.com/sdk/server-side/go) and
[Go AI SDK](https://launchdarkly.com/docs/sdk/ai/go).

## The scenario

ABC Company ships Kubescope, a Kubernetes security scanner. Today it runs a **legacy static
scanner** (image/CVE scanning only). The team has built a **new runtime engine** that adds
behavioral detection (unexpected shell exec, outbound connections to bad IPs, etc.) and re-scores
static findings based on runtime exploitability — fewer false positives, plus new true positives
static scanning can't see. This app is the dashboard where that rollout happens.

| Exercise requirement | Where it lives here |
|---|---|
| Part 1 — Feature Flag | `new-scan-engine-enabled` toggles legacy vs. runtime engine findings |
| Part 1 — Instant release/rollback | Server-Sent Events (`/events`) push a refresh the instant the flag changes — no page reload |
| Part 1 — Remediate via trigger | An LD Trigger (webhook) flips the flag off; fire it with `curl` to simulate an incident rollback |
| Part 2 — Context attributes | A `cluster` context: `environment`, `plan`, `team`, `region` |
| Part 2 — Individual targeting | The `internal-dogfood-eng` persona is targeted directly |
| Part 2 — Rule-based targeting | A rule like `environment == production AND plan == enterprise` |
| Extra credit — Experimentation | `Resolve` / `False positive` buttons fire `finding-resolved` / `finding-marked-false-positive` events, used as experiment metrics |
| Extra credit — AI Configs | The "AI remediation advisor" panel is powered by an LD AI Config (`k8s-remediation-advisor`) managing the prompt + model |

No real Kubernetes cluster is involved — findings are mock fixtures shaped like real
Trivy/Falco-style output (`internal/scanner/data/*.json`), so the whole thing runs anywhere with
just Go installed.

## Architecture notes

- Flags are evaluated **server-side** (Go SDK), not in the browser. In a real security product you
  generally don't want targeting rules/segments exposed in a client-side JS bundle, so the server
  evaluates on behalf of whichever "cluster" context is selected and returns only the result.
- "Instant switch, no reload" is implemented with the SDK's `FlagTracker.AddFlagChangeListener()`:
  whenever the flag's configuration changes, the server broadcasts a message over a long-lived SSE
  connection, and the page re-fetches its data and re-renders in place.
- The frontend (`web/static/`) is plain HTML/CSS/JS — no build step — so setup is just "run the Go
  binary."

## Prerequisites

- Go 1.22+
- A LaunchDarkly account ([free trial](https://launchdarkly.com/start-trial/))
- Optional: an [Anthropic API key](https://console.anthropic.com/) if you want the AI Config panel
  to call a real model instead of falling back to a canned response

## 1. LaunchDarkly project setup

Do this once, in your LD dashboard, before running the app.

### Context kind

Create a context kind with key `cluster` (Account settings → Context kinds). No special config
needed beyond the key existing — it's used automatically once a flag/AI Config references it.

### Flag: `new-scan-engine-enabled`

Create a **boolean** flag with key `new-scan-engine-enabled`.

- Default rule (fallthrough): serve `false` (legacy scanner) to everyone.
- **Individual targeting:** under Targeting, add an individual target for context kind `cluster`,
  key `internal-dogfood-eng` → serve `true`. This is your internal dogfood cluster that always
  gets the new engine early.
- **Rule-based targeting:** add a rule — `environment` is one of `production` AND `plan` is one of
  `enterprise` → serve `true`. This rolls the new engine out to your enterprise production
  customers first.
- Turn the flag **on** to activate targeting (with the flag off entirely, everyone gets the
  off-variation regardless of rules).

### Trigger (Part 1 — remediate)

On the same flag, add a **Trigger** (Flag → three-dot menu → "Add Trigger" / Integrations →
Triggers) that sets the flag to `false` (off) when invoked. Copy the generated webhook URL.

To simulate an incident rollback:

```bash
curl -X POST "<your-trigger-webhook-url>"
```

Watch the running app's dashboard update instantly — no redeploy, no reload.

### Metrics + Experiment (extra credit)

Create two metrics (Metrics → Create metric), both **occurrence** metrics on custom event:

- `finding-resolved` (higher is better — engine surfaces real, actionable findings)
- `finding-marked-false-positive` (lower is better — fewer wasted investigations)

Then create an **Experiment** on `new-scan-engine-enabled` using these metrics. Run the app,
switch between personas, and click "Resolve" / "False positive" on findings to generate events —
do this a number of times across both engine variations (e.g. toggle targeting so different
personas land on different variations) to get enough data for the experiment to show a trend.

### AI Config (extra credit)

Create an AI Config with key `k8s-remediation-advisor` (Account settings → AI Configs). Add at
least one variation with:

- A system message: something like _"You are a Kubernetes security remediation assistant. Given a
  finding, respond with 3 concise, numbered remediation steps. Be specific to Kubernetes."_
- A user message template using the variables this app supplies:
  `Finding: {{finding_title}} (severity: {{finding_severity}}) on {{resource}}. CVE: {{cve}}. Details: {{finding_description}}`
- A model name matching an Anthropic model you have access to (e.g. `claude-haiku-4-5` or
  `claude-sonnet-5`)

Add a second variation with a different prompt and/or model to demonstrate swapping them live —
e.g. a "detailed steps + `claude-sonnet-5`" variation vs. a "concise bullets + `claude-haiku-4-5`"
variation. Target/rollout between them however you like.

If you skip this section entirely, the app still runs fine — the advisor panel falls back to a
canned static remediation message and clearly labels itself as such (`source: fallback-*` in the
response).

## 2. Run it locally

```bash
git clone <this-repo>
cd kubescope
cp .env.example .env
# edit .env: set LD_SDK_KEY to your environment's server-side SDK key
#            (optional) set ANTHROPIC_API_KEY to enable real AI Config completions

go run ./cmd/server
```

Then open [http://localhost:8080](http://localhost:8080).

Assumptions:

- You have Go 1.22+ on your `PATH`.
- Port 8080 is free (override with `PORT=xxxx go run ./cmd/server`).
- No database, no cluster, no other services required.

## 3. Demo script

1. **Release/rollback (Part 1):** Open the app, select the `internal-dogfood-eng` persona from
   the dropdown — engine badge shows "Runtime Engine v2" because it's individually targeted. Go
   to the LD dashboard and toggle the flag off; watch the dashboard flip to "Legacy Static
   Scanner" within about a second, with no reload.
2. **Remediate (Part 1):** With the flag back on, fire the Trigger webhook via `curl` and watch
   the same instant flip happen from an "operational" action instead of the dashboard toggle.
3. **Targeting (Part 2):** Switch personas in the dropdown — `acme-prod-01` (enterprise/production)
   should get the runtime engine via the rule; `initech-dev-01` (free/development) should not.
4. **Experimentation (extra credit):** Click "Resolve"/"False positive" on a few findings per
   persona, then check the Experiment results in LD after generating enough events.
5. **AI Configs (extra credit):** Click "AI remediation" on any finding; the modal shows the
   generated remediation plus which model/variation/config served it. Change the AI Config's
   default variation in LD and click again to see the new prompt/model take effect immediately.

## Project layout

```
cmd/server/main.go          HTTP server, LD client wiring, routes
internal/scanner/           Mock finding fixtures + engine selection logic
internal/ldcontexts/        Demo "cluster" personas and LD context construction
internal/aiadvisor/         AI Config integration for the remediation panel
internal/sse/               Minimal Server-Sent Events broadcast hub
web/static/                 Plain HTML/CSS/JS frontend, no build step
```
