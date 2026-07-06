// Package ldcontexts defines the demo "cluster" personas used to exercise
// LaunchDarkly individual and rule-based targeting (Part 2 of the exercise).
//
// Each persona models a customer's Kubernetes cluster as an LD context of kind
// "cluster". In a real integration this context would be built from whatever
// your platform already knows about the cluster/tenant making the request
// (e.g. read from your control plane or customer database) rather than a
// hardcoded list.
package ldcontexts

import (
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
)

// ContextKind is the LD context kind used throughout this demo. Using a
// custom kind (instead of the default "user") reflects that targeting here
// is really about *which cluster* is asking, not which human is logged in.
const ContextKind = ldcontext.Kind("cluster")

// Persona is a demo cluster profile. The attributes below are what Part 2's
// rule-based targeting rules match against (environment, plan) and what
// individual targeting matches against (Key).
type Persona struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Environment string `json:"environment"` // production | staging | development
	Plan        string `json:"plan"`        // enterprise | team | free
	Team        string `json:"team"`
	Region      string `json:"region"`
}

// Personas is the fixed list of demo clusters selectable from the dashboard's
// context switcher. "internal-dogfood-eng" is the one meant to be added as an
// LD *individual target* so it always gets the new engine regardless of the
// rule-based rollout below.
var Personas = []Persona{
	{
		Key:         "internal-dogfood-eng",
		Name:        "Macrodata Refinement",
		Environment: "production",
		Plan:        "enterprise",
		Team:        "platform-security",
		Region:      "us-east-1",
	},
	{
		Key:         "acme-prod-01",
		Name:        "Optics and Design (Production)",
		Environment: "production",
		Plan:        "enterprise",
		Team:        "customer",
		Region:      "us-east-1",
	},
	{
		Key:         "acme-staging-01",
		Name:        "Optics and Design (Staging)",
		Environment: "staging",
		Plan:        "enterprise",
		Team:        "customer",
		Region:      "us-east-1",
	},
	{
		Key:         "globex-prod-01",
		Name:        "Mammalians Nurturable (Production)",
		Environment: "production",
		Plan:        "team",
		Team:        "customer",
		Region:      "eu-west-1",
	},
	{
		Key:         "initech-dev-01",
		Name:        "Choreography and Merriment (Development)",
		Environment: "development",
		Plan:        "free",
		Team:        "customer",
		Region:      "us-west-2",
	},
}

// Find returns the persona with the given key, defaulting to the first
// persona if the key is unknown or empty.
func Find(key string) Persona {
	for _, p := range Personas {
		if p.Key == key {
			return p
		}
	}
	return Personas[0]
}

// ToLDContext converts a Persona into the LD context evaluated for every
// flag/AI Config request the dashboard makes on its behalf.
func (p Persona) ToLDContext() ldcontext.Context {
	return ldcontext.NewBuilder(p.Key).
		Kind(ContextKind).
		Name(p.Name).
		SetString("environment", p.Environment).
		SetString("plan", p.Plan).
		SetString("team", p.Team).
		SetString("region", p.Region).
		Build()
}
