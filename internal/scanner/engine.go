// Package scanner provides mock Kubernetes vulnerability/runtime findings data,
// standing in for a real scanner (e.g. Trivy for static image scanning, Falco-style
// eBPF sensors for runtime detection) so the demo has no dependency on a live cluster.
package scanner

import (
	"embed"
	"encoding/json"
)

//go:embed data/legacy_findings.json data/runtime_findings.json
var dataFS embed.FS

// Finding mirrors the shape of a typical scanner finding (severity, resource,
// CVE, description) so the demo UI reads like a real security product.
type Finding struct {
	ID          string `json:"id"`
	Resource    string `json:"resource"`
	Namespace   string `json:"namespace"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	CVE         string `json:"cve"`
	Source      string `json:"source"`
}

// Engine identifies which scan engine produced a set of findings. This is the
// concept the "new-scan-engine-enabled" feature flag toggles between.
type Engine string

const (
	EngineLegacy  Engine = "legacy-static-scanner"
	EngineRuntime Engine = "runtime-engine-v2"
)

var (
	legacyFindings  []Finding
	runtimeFindings []Finding
)

func init() {
	legacyFindings = mustLoad("data/legacy_findings.json")
	runtimeFindings = mustLoad("data/runtime_findings.json")
}

func mustLoad(path string) []Finding {
	raw, err := dataFS.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var findings []Finding
	if err := json.Unmarshal(raw, &findings); err != nil {
		panic(err)
	}
	return findings
}

// FindingsFor returns the mock findings for the given engine.
func FindingsFor(engine Engine) []Finding {
	if engine == EngineRuntime {
		return runtimeFindings
	}
	return legacyFindings
}

// FindByID looks up a finding by ID across both engines' fixture sets, so the
// AI advisor endpoint can resolve a finding regardless of which engine the
// caller currently has active.
func FindByID(id string) (Finding, bool) {
	for _, f := range legacyFindings {
		if f.ID == id {
			return f, true
		}
	}
	for _, f := range runtimeFindings {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

// Summary is a small severity rollup used by the dashboard header.
type Summary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

func Summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Severity {
		case "Critical":
			s.Critical++
		case "High":
			s.High++
		case "Medium":
			s.Medium++
		case "Low":
			s.Low++
		}
	}
	return s
}
