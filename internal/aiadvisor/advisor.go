// Package aiadvisor implements the "AI Configs" extra credit: an AI-generated
// remediation suggestion for a security finding, with the prompt template and
// model managed by a LaunchDarkly AI Config instead of being hardcoded.
//
// It degrades gracefully in two situations so the rest of the demo keeps
// working even if this part isn't fully set up:
//  1. If the AI Config named by ConfigKey doesn't exist yet in your LD
//     project, CompletionConfig returns the fallbackConfig below (Enabled).
//  2. If ANTHROPIC_API_KEY isn't set, we still evaluate the AI Config and
//     interpolate the prompt (proving the LD wiring works), but skip the
//     real model call and return a canned remediation string instead.
package aiadvisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/launchdarkly/go-server-sdk/ldai"
	"github.com/launchdarkly/go-server-sdk/ldai/datamodel"
	ld "github.com/launchdarkly/go-server-sdk/v7"

	"github.com/emagnus/kubescope/internal/ldcontexts"
	"github.com/emagnus/kubescope/internal/scanner"
)

// ConfigKey must match the AI Config key you create in the LaunchDarkly
// dashboard (Account settings > AI Configs). See README for the exact
// variations to create.
const ConfigKey = "k8s-remediation-advisor"

// Advisory is what the dashboard renders in the "AI Remediation Advisor" panel.
type Advisory struct {
	Text      string `json:"text"`
	Model     string `json:"model"`
	Variation string `json:"variation"`
	Source    string `json:"source"` // "llm" | "fallback-disabled" | "fallback-no-api-key" | "fallback-error"
}

type Advisor struct {
	ai           *ldai.Client
	httpClient   *http.Client
	anthropicKey string
}

func New(ldClient *ld.LDClient) (*Advisor, error) {
	aiClient, err := ldai.NewClient(ldClient)
	if err != nil {
		return nil, fmt.Errorf("creating AI Config client: %w", err)
	}
	return &Advisor{
		ai:           aiClient,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		anthropicKey: os.Getenv("ANTHROPIC_API_KEY"),
	}, nil
}

// fallbackConfig is used only if the AI Config key doesn't exist yet in your
// LD project (e.g. before you've finished setting it up).
func fallbackConfig() ldai.Config {
	return ldai.NewConfig().
		WithEnabled(true).
		WithModelName("fallback-static-advisor").
		WithMessage(
			"You are a Kubernetes security remediation assistant. Given a finding, "+
				"respond with 3 concise, numbered remediation steps. Be specific to Kubernetes.",
			datamodel.System,
		).
		WithMessage(
			"Finding: {{finding_title}} (severity: {{finding_severity}}) on {{resource}}. "+
				"CVE: {{cve}}. Details: {{finding_description}}",
			datamodel.User,
		).
		Build()
}

// CurrentVariation reports which AI Config variation/model is currently
// active for the given persona, without making a real model call -- just the
// config lookup. Used to show the active config in the dashboard header.
func (a *Advisor) CurrentVariation(persona ldcontexts.Persona) (variation string, model string, enabled bool) {
	ldContext := persona.ToLDContext()
	cfg := a.ai.CompletionConfig(ConfigKey, ldContext, fallbackConfig(), nil)
	return cfg.VariationKey(), cfg.ModelName(), cfg.Enabled()
}

// Remediate evaluates the AI Config for the given persona/finding, calls the
// configured model if possible, and always returns an Advisory (never leaves
// the UI empty-handed).
func (a *Advisor) Remediate(persona ldcontexts.Persona, f scanner.Finding) Advisory {
	ldContext := persona.ToLDContext()
	variables := map[string]interface{}{
		"finding_title":       f.Title,
		"finding_severity":    f.Severity,
		"finding_description": f.Description,
		"resource":            f.Resource,
		"cve":                 f.CVE,
	}

	cfg := a.ai.CompletionConfig(ConfigKey, ldContext, fallbackConfig(), variables)
	tracker := cfg.CreateTracker()

	if !cfg.Enabled() {
		return Advisory{
			Text:      staticRemediation(f),
			Model:     "none (AI Config disabled for this context)",
			Variation: cfg.VariationKey(),
			Source:    "fallback-disabled",
		}
	}

	if a.anthropicKey == "" {
		return Advisory{
			Text:      staticRemediation(f),
			Model:     cfg.ModelName(),
			Variation: cfg.VariationKey(),
			Source:    "fallback-no-api-key",
		}
	}

	var completion string
	_, err := tracker.TrackRequest(func(c *ldai.Config) (ldai.ProviderResponse, error) {
		text, usage, latency, callErr := a.callAnthropic(c.ModelName(), c.Messages())
		if callErr != nil {
			return ldai.ProviderResponse{}, callErr
		}
		completion = text
		return ldai.ProviderResponse{
			Usage:   usage,
			Metrics: ldai.Metrics{Latency: latency},
		}, nil
	})
	if err != nil {
		return Advisory{
			Text:      staticRemediation(f) + fmt.Sprintf("\n\n(LLM call failed, showing fallback: %v)", err),
			Model:     cfg.ModelName(),
			Variation: cfg.VariationKey(),
			Source:    "fallback-error",
		}
	}

	return Advisory{
		Text:      completion,
		Model:     cfg.ModelName(),
		Variation: cfg.VariationKey(),
		Source:    "llm",
	}
}

func staticRemediation(f scanner.Finding) string {
	return fmt.Sprintf(
		"1. Triage %s in namespace %s and confirm whether %s is exposed to external traffic.\n"+
			"2. Patch or replace the affected image layer, then redeploy via your normal rollout process.\n"+
			"3. Add a policy (e.g. OPA/Gatekeeper or Kyverno) to prevent this class of misconfiguration from recurring.",
		f.Resource, f.Namespace, f.Resource,
	)
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// callAnthropic makes a minimal, dependency-free call to the Anthropic
// Messages API using whatever model/system/user messages the AI Config
// resolved to. Swap this out for the official anthropic-sdk-go, OpenAI, or
// any other provider if your AI Config points at a different one.
func (a *Advisor) callAnthropic(model string, messages []datamodel.Message) (string, ldai.TokenUsage, time.Duration, error) {
	var system string
	var chatMessages []anthropicMessage
	for _, m := range messages {
		switch m.Role {
		case datamodel.System:
			system = m.Content
		case datamodel.User:
			chatMessages = append(chatMessages, anthropicMessage{Role: "user", Content: m.Content})
		case datamodel.Assistant:
			chatMessages = append(chatMessages, anthropicMessage{Role: "assistant", Content: m.Content})
		}
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: 512,
		System:    system,
		Messages:  chatMessages,
	})
	if err != nil {
		return "", ldai.TokenUsage{}, 0, err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", ldai.TokenUsage{}, 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.anthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", ldai.TokenUsage{}, 0, err
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ldai.TokenUsage{}, latency, err
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", ldai.TokenUsage{}, latency, err
	}
	if parsed.Error != nil {
		return "", ldai.TokenUsage{}, latency, fmt.Errorf("anthropic API error: %s", parsed.Error.Message)
	}
	if len(parsed.Content) == 0 {
		return "", ldai.TokenUsage{}, latency, fmt.Errorf("anthropic API returned no content (status %d)", resp.StatusCode)
	}

	usage := ldai.TokenUsage{
		Input:  parsed.Usage.InputTokens,
		Output: parsed.Usage.OutputTokens,
		Total:  parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	return parsed.Content[0].Text, usage, latency, nil
}
