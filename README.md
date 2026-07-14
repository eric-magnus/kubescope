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

## Environmental requirements

- Go 1.22+
- [Anthropic key](https://console.anthropic.com/) - put this in your .env

## Setting up LaunchDarkly

Do this in the LD project:

**SDK key** - grab your environment's server-side SDK key (Account settings → Projects → your project → your environment). Put this in `.env` as `LD_SDK_KEY`, use .env.example for the format.

**Context kind** - create one with key `cluster` (Account settings → Context kinds)

**The flag** - create a boolean flag, key `new-scan-engine-enabled`.

- Fallthrough / off variation: `false`
- Individual target: context kind `cluster`, key `internal-dogfood-eng` → `true` (this is
  "Macrodata Refinement," the internal cluster that always gets the new engine)
- Rule: `environment` is one of `production` AND `plan` is one of `enterprise` → `true` (turns the flag on for any clusters besides Macrodata Refinement are prod & enterprise). 
- Turn the flag on - targeting rules do nothing while it's off

**Trigger** - add one to the same flag that sets it to `false`. This will be used by the ops teams via automation to disable the new engine depending on backend metrics (there are concerns around infra stability with the new engine). Verified works.

```bash
curl -X POST "<your-trigger-webhook-url>"
```

**Metrics + experiment (extra credit)** - create two occurrence metrics: `finding-resolved`
(higher is better) and `finding-marked-false-positive` (lower is better). Both need `cluster` as
their randomization unit (the sentence-builder's "per user" dropdown defaults to `user` - change
it). Then set up an Experiment on `new-scan-engine-enabled` using them, with `cluster` as
"Randomize by".

Clicking Resolve/False positive manually can't generate enough data: these are occurrence
metrics, so they count unique converting units, not click volume, and Kubescope only has 5 fixed
demo personas - the sample size caps at 5 no matter how many times you click. Instead, run:

```bash
go run ./cmd/seed
```

This generates ~400 synthetic cluster contexts, evaluates the real flag for each, and fires
Resolve/False-positive events with a deliberate skew (runtime engine: fewer false positives, more
resolved findings) so the experiment has enough exposures per treatment to say something
meaningful within seconds.

**AI Config (extra credit)** - create one with key `k8s-remediation-advisor`. Add a variation
with:

- System message: _"You are a Kubernetes security remediation assistant. Given a finding, respond
  with 3 concise, numbered remediation steps. Be specific to Kubernetes."_
- User message: `Finding: {{finding_title}} (severity: {{finding_severity}}) on {{resource}}. CVE: {{cve}}. Details: {{finding_description}}`
- Use claude-haiku-4-5 model
- Add a variation using claude-sonnet5, with the following system message: _"You are a senior Kubernetes security engineer conducting an incident review. Given a finding, provide a thorough, step-by-step remediation plan: relevant kubectl commands, applicable controls (Pod Security Standards, NetworkPolicies, Falco rules, OPA/Gatekeeper or Kyverno policies), and a brief note on the underlying risk. Be comprehensive, not brief."_
- User message is the same as the haiku model

## Running it

```bash
git clone <this-repo>
cd kubescope
cp .env.example .env

go run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080). Needs Go on your PATH and port 8080 free
(or set `PORT` to something else)

## Layout

```
cmd/server/main.go          HTTP server, LD client wiring, routes
cmd/seed/main.go            Synthetic traffic generator for the Experimentation extra credit
internal/scanner/           Mock finding fixtures + engine selection logic
internal/ldcontexts/        Demo "cluster" personas and LD context construction
internal/aiadvisor/         AI Config integration for the remediation panel
internal/sse/               Minimal Server-Sent Events broadcast hub
web/static/                 Plain HTML/CSS/JS frontend, no build step
```
