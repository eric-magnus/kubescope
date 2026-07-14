// Command server runs Kubescope: a mock Kubernetes vulnerability/runtime-findings
// dashboard used to exercise LaunchDarkly feature flags, targeting,
// experimentation, and AI Configs. See README.md for the full LaunchDarkly
// project setup this expects.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	ld "github.com/launchdarkly/go-server-sdk/v7"

	"github.com/emagnus/kubescope/internal/aiadvisor"
	"github.com/emagnus/kubescope/internal/ldcontexts"
	"github.com/emagnus/kubescope/internal/scanner"
	"github.com/emagnus/kubescope/internal/sse"
)

// FlagScanEngine is the Part 1 / Part 2 flag: false serves the legacy static
// scanner's findings, true serves the new runtime engine's findings. Create
// this as a boolean flag with this exact key in your LD project.
const FlagScanEngine = "new-scan-engine-enabled"

// Event keys for the Experimentation extra credit. Create LD metrics with
// these same keys (occurrence/conversion metrics work well here).
const (
	MetricFalsePositive = "finding-marked-false-positive"
	MetricResolved      = "finding-resolved"
)

func main() {
	_ = godotenv.Load()

	sdkKey := os.Getenv("LD_SDK_KEY")
	if sdkKey == "" {
		log.Fatal("LD_SDK_KEY is not set. Copy .env.example to .env and fill in your LaunchDarkly server-side SDK key.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ldClient, err := ld.MakeClient(sdkKey, 5*time.Second)
	if err != nil {
		log.Fatalf("failed to initialize LaunchDarkly client: %v", err)
	}
	defer ldClient.Close()

	advisor, err := aiadvisor.New(ldClient)
	if err != nil {
		log.Fatalf("failed to initialize AI Config client: %v", err)
	}

	hub := sse.NewHub()

	// Any change to the flag's configuration (toggled on/off, targeting rules
	// edited, a Trigger fired, etc.) broadcasts a "refresh" message to every
	// connected browser, which re-fetches its dashboard data. That's what
	// makes the release/rollback feel instant with no page reload.
	go func() {
		changes := ldClient.GetFlagTracker().AddFlagChangeListener()
		for ev := range changes {
			if ev.Key == FlagScanEngine {
				log.Printf("flag changed: %s -> broadcasting refresh", ev.Key)
				hub.Broadcast("refresh")
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "web/static/index.html")
	})
	mux.HandleFunc("/events", hub.ServeHTTP)
	mux.HandleFunc("/api/personas", handlePersonas)
	mux.HandleFunc("/api/dashboard", handleDashboard(ldClient, advisor))
	mux.HandleFunc("/api/triage", handleTriage(ldClient))
	mux.HandleFunc("/api/advisor", handleAdvisor(advisor))

	log.Printf("Kubescope listening on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func handlePersonas(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ldcontexts.Personas)
}

type dashboardResponse struct {
	Persona     ldcontexts.Persona `json:"persona"`
	Engine      scanner.Engine     `json:"engine"`
	Reason      string             `json:"reason"`
	Summary     scanner.Summary    `json:"summary"`
	Findings    []scanner.Finding  `json:"findings"`
	AIVariation string             `json:"aiVariation"`
	AIModel     string             `json:"aiModel"`
	AIEnabled   bool               `json:"aiEnabled"`
}

func handleDashboard(ldClient *ld.LDClient, advisor *aiadvisor.Advisor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		persona := ldcontexts.Find(r.URL.Query().Get("persona"))
		ldContext := persona.ToLDContext()

		useRuntimeEngine, detail, err := ldClient.BoolVariationDetail(FlagScanEngine, ldContext, false)
		if err != nil {
			log.Printf("flag evaluation error: %v", err)
		}

		engine := scanner.EngineLegacy
		if useRuntimeEngine {
			engine = scanner.EngineRuntime
		}
		findings := scanner.FindingsFor(engine)

		aiVariation, aiModel, aiEnabled := advisor.CurrentVariation(persona)

		writeJSON(w, http.StatusOK, dashboardResponse{
			Persona:     persona,
			Engine:      engine,
			Reason:      detail.Reason.String(),
			Summary:     scanner.Summarize(findings),
			Findings:    findings,
			AIVariation: aiVariation,
			AIModel:     aiModel,
			AIEnabled:   aiEnabled,
		})
	}
}

type triageRequest struct {
	PersonaKey string `json:"personaKey"`
	FindingID  string `json:"findingId"`
	Action     string `json:"action"` // "resolved" | "false_positive"
}

func handleTriage(ldClient *ld.LDClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req triageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		persona := ldcontexts.Find(req.PersonaKey)
		ldContext := persona.ToLDContext()

		var eventKey string
		switch req.Action {
		case "false_positive":
			eventKey = MetricFalsePositive
		case "resolved":
			eventKey = MetricResolved
		default:
			http.Error(w, "action must be 'resolved' or 'false_positive'", http.StatusBadRequest)
			return
		}

		// Tracking against the same context that evaluated FlagScanEngine is
		// what lets an LD Experiment attribute this event back to whichever
		// engine variation this cluster was on.
		if err := ldClient.TrackEvent(eventKey, ldContext); err != nil {
			log.Printf("track event error: %v", err)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleAdvisor(advisor *aiadvisor.Advisor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		persona := ldcontexts.Find(r.URL.Query().Get("persona"))
		findingID := r.URL.Query().Get("findingId")

		finding, ok := scanner.FindByID(findingID)
		if !ok {
			http.Error(w, "unknown findingId", http.StatusNotFound)
			return
		}

		advisory := advisor.Remediate(persona, finding)
		writeJSON(w, http.StatusOK, advisory)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}
