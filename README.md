# Kubescope - LaunchDarkly SE Technical Exercise

Kubescope is a fake Kubernetes security dashboard I built to work through the LaunchDarkly SE
exercise: flags, targeting, experimentation, and AI Configs, all in Go against the real LD SDKs.

## The idea

Lumon Industries runs Kubescope internally to scan its clusters. Right now it's a **legacy static
scanner** - basically image/CVE scanning. There's a **new runtime engine** in the works that adds
behavioral detection (unexpected shell exec, weird outbound connections, that kind of thing, based on Falco) and
re-scores static findings based on whether they're actually exploitable at runtime, giving fewer false
positives, plus it catches stuff static scanning just can't see. "In-use" vulnerabilities derived from runtime data are a critical step towards true Attack Surface Management.

This is loosely based on a real rollout I lived through at a SaaS security vendor, where a new
scanning engine went out to every customer at once and chaos ensued. Hence the flag.

Excercise Requirements > where they live:

- **Flag** > `new-scan-engine-enabled`, boolean, toggles legacy vs. runtime findings
- **Instant release/rollback** > the flag change listener pushes over SSE (`/events`), so the
  dashboard updates the second you flip it, no reload
- **Remediate via trigger** > an LD Trigger flips the flag off; `curl` it to simulate an on-call
  engineer killing a bad release
- **Context attributes** > a `cluster` context: `environment`, `plan`, `team`, `region`
- **Individual targeting** > `internal-dogfood-eng` ("Macrodata Refinement") is targeted directly
- **Rule-based targeting** > `environment is production AND plan is enterprise` - currently, this is the "Optics & Design Production" cluster. Individual targets take priority over rules, so if you add that specific cluster as a target serving false, it would override the rule.
- **Experimentation** > Resolve / False positive buttons fire
  `finding-resolved`/`finding-marked-false-positive` events for use as experiment metrics. These are visible under Data>Event Explorer
- **AI Configs** > the AI remediation panel is backed by an LD AI Config
  (`k8s-remediation-advisor`) that controls the prompt and model

This is currently mock data - findings come from JSON fixtures shaped like real Trivy/Falco output
(`internal/scanner/data/*.json`). No cluster required, no dependencies beyond Go and an LD account. For v2, would like to connect to live k8s clusters via kubeapi.

## What you need

- Go 1.22+
- An LD account - (https://launchdarkly.com/start-trial/)
- [Anthropic key](https://console.anthropic.com/) Optional if you want the AI advisor
  actually calling a model instead of returning canned text

## Setting up LaunchDarkly

Do this once in your LD project before running the app.

**SDK key** - grab your environment's server-side SDK key (Account settings → Projects → your project → your environment). You'll put this in `.env` as `LD_SDK_KEY` in the next section.

**Context kind** - create one with key `cluster` (Account settings → Context kinds). Nothing
fancy needed, it just has to exist.

**The flag** - create a boolean flag, key `new-scan-engine-enabled`.

- Fallthrough / off variation: `false`
- Individual target: context kind `cluster`, key `internal-dogfood-eng` → `true` (this is
  "Macrodata Refinement," the internal cluster that always gets the new engine)
- Rule: `environment` is one of `production` AND `plan` is one of `enterprise` → `true`
- Turn the flag on - targeting rules do nothing while it's off

**Trigger** - add one to the same flag that sets it to `false`. Grab the webhook URL it gives
you (LD only shows it once, so copy it before navigating away). Firing it should feel like an
incident rollback:

```bash
curl -X POST "<your-trigger-webhook-url>"
```

**Metrics + experiment (extra credit)** - create two occurrence metrics: `finding-resolved`
(higher is better) and `finding-marked-false-positive` (lower is better). Then set up an
Experiment on `new-scan-engine-enabled` using them. This unfortunately takes a lot of clicks to generate enough data for the Experiment view, but you can see the results in Metrics>Event Explorer

**AI Config (extra credit)** - create one with key `k8s-remediation-advisor`. Add a variation
with:

- System message: _"You are a Kubernetes security remediation assistant. Given a finding, respond
  with 3 concise, numbered remediation steps. Be specific to Kubernetes."_
- User message: `Finding: {{finding_title}} (severity: {{finding_severity}}) on {{resource}}. CVE: {{cve}}. Details: {{finding_description}}`
- A real Anthropic model, e.g. `claude-haiku-4-5` or `claude-sonnet-5`

Add a second variation with a different prompt or model if you want to show off swapping them
live. If you skip this whole section the app still works fine - the advisor just falls back to a
canned response and says so (`source: fallback-*`).

I created a concise variation with haiku, and a detailed one using sonnet.

## Running it

```bash
git clone <this-repo>
cd kubescope
cp .env.example .env
# fill in LD_SDK_KEY (and ANTHROPIC_API_KEY if you want real completions)

go run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080). Needs Go on your PATH and port 8080 free
(or set `PORT` to something else)

## Demo flow

1. **Release/rollback** - pick "Macrodata Refinement" from the dropdown, it's on the runtime
   engine (individually targeted). Flip the flag off in LD, watch it switch to the legacy scanner
   in about a second, no reload.
2. **Remediate** - flag back on, then hit the trigger URL with curl instead. Same instant flip,
   different trigger.
3. **Targeting** - "Optics and Design (Production)" should land on the runtime engine via the
   rule; "Choreography and Merriment (Development)" shouldn't.
4. **Experimentation** - click Resolve / False positive a bunch across personas, check the
   experiment results in LD once there's enough data.
5. **AI Configs** - click "AI remediation" on a finding, see the real model response plus which
   config/variation served it. Change the default variation in LD, click again, watch it change.

## Layout

```
cmd/server/main.go          HTTP server, LD client wiring, routes
internal/scanner/           Mock finding fixtures + engine selection logic
internal/ldcontexts/        Demo "cluster" personas and LD context construction
internal/aiadvisor/         AI Config integration for the remediation panel
internal/sse/               Minimal Server-Sent Events broadcast hub
web/static/                 Plain HTML/CSS/JS frontend, no build step
```
