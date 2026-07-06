// No framework, no build step: this is a plain page so the setup instructions
// stay "clone, run the Go binary, open a browser."
//
// The "instant switch, no reload" requirement (Part 1) is implemented as:
//   1. /events is a long-lived Server-Sent Events connection.
//   2. The server pushes a "refresh" message whenever the scan-engine flag's
//      configuration changes (toggled, targeting edited, a Trigger fired).
//   3. On that message, the page just re-fetches /api/dashboard and re-renders
//      the DOM in place. No location.reload() anywhere.

const personaSelect = document.getElementById("persona-select");
const engineBadge = document.getElementById("engine-badge");
const evalReason = document.getElementById("eval-reason");
const findingsBody = document.getElementById("findings-body");
const sseStatus = document.getElementById("sse-status");
const advisorModal = document.getElementById("advisor-modal");
const advisorMeta = document.getElementById("advisor-meta");
const advisorText = document.getElementById("advisor-text");

let currentPersona = null;

async function loadPersonas() {
  const res = await fetch("/api/personas");
  const personas = await res.json();
  personaSelect.innerHTML = personas
    .map((p) => `<option value="${p.key}">${p.name}</option>`)
    .join("");
  currentPersona = personas[0].key;
}

async function loadDashboard() {
  const res = await fetch(`/api/dashboard?persona=${encodeURIComponent(currentPersona)}`);
  const data = await res.json();
  render(data);
}

function render(data) {
  const isRuntime = data.engine === "runtime-engine-v2";
  engineBadge.textContent = isRuntime ? "Runtime Engine v2" : "Legacy Static Scanner";
  engineBadge.className = `badge ${isRuntime ? "runtime" : "legacy"}`;
  evalReason.textContent = data.reason;

  document.getElementById("count-critical").textContent = data.summary.critical;
  document.getElementById("count-high").textContent = data.summary.high;
  document.getElementById("count-medium").textContent = data.summary.medium;
  document.getElementById("count-low").textContent = data.summary.low;

  findingsBody.innerHTML = data.findings
    .map(
      (f) => `
    <tr data-id="${f.id}">
      <td><span class="sev-pill sev-${f.severity}">${f.severity}</span></td>
      <td>${f.resource}<br><small>${f.namespace}</small></td>
      <td>
        <div class="finding-title">${f.title}</div>
        <div class="finding-desc">${f.description}</div>
      </td>
      <td>${f.cve || "&mdash;"}</td>
      <td>${f.source}</td>
      <td>
        <button class="primary" data-action="advisor">AI remediation</button><br>
        <button data-action="resolved">Resolve</button>
        <button data-action="false_positive">False positive</button>
      </td>
    </tr>`
    )
    .join("");
}

async function triage(findingId, action) {
  await fetch("/api/triage", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ personaKey: currentPersona, findingId, action }),
  });
}

async function showAdvisor(findingId) {
  advisorModal.showModal();
  advisorMeta.textContent = "Asking the AI Config for a remediation suggestion…";
  advisorText.textContent = "Loading…";

  const res = await fetch(
    `/api/advisor?persona=${encodeURIComponent(currentPersona)}&findingId=${encodeURIComponent(findingId)}`
  );
  const advisory = await res.json();
  advisorMeta.textContent = `model: ${advisory.model} · variation: ${advisory.variation} · source: ${advisory.source}`;
  advisorText.textContent = advisory.text;
}

findingsBody.addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  const row = event.target.closest("tr");
  const findingId = row.dataset.id;
  const action = button.dataset.action;

  if (action === "advisor") {
    showAdvisor(findingId);
  } else {
    triage(findingId, action);
    button.textContent = action === "resolved" ? "Resolved ✓" : "Marked ✓";
    button.disabled = true;
  }
});

personaSelect.addEventListener("change", () => {
  currentPersona = personaSelect.value;
  loadDashboard();
});

function connectEvents() {
  const source = new EventSource("/events");
  source.onopen = () => sseStatus.classList.add("connected");
  source.onerror = () => sseStatus.classList.remove("connected");
  source.onmessage = (event) => {
    if (event.data === "refresh") {
      loadDashboard();
    }
  };
}

(async function init() {
  await loadPersonas();
  await loadDashboard();
  connectEvents();
})();
