// Command seed generates synthetic traffic for the Experimentation extra
// credit. Manually clicking Resolve/False positive in the app can't reach
// statistical significance: Kubescope only ships 5 fixed demo personas
// (internal/ldcontexts), and an occurrence metric counts unique converting
// units, not click volume -- 500 clicks from the same 5 clusters is still
// only 5 units of sample size. LD wants ~100 exposures per treatment.
//
// This generates many one-off synthetic cluster contexts, evaluates the real
// flag for each (so LD's experiment can correlate exposure -> variation), and
// fires Resolve/False-positive events with a deliberate skew matching the
// demo's premise: the runtime engine produces fewer false positives and
// resolves more real findings than the legacy scanner.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ld "github.com/launchdarkly/go-server-sdk/v7"

	"github.com/emagnus/kubescope/internal/ldcontexts"
)

const (
	flagKey             = "new-scan-engine-enabled"
	metricResolved      = "finding-resolved"
	metricFalsePositive = "finding-marked-false-positive"
	defaultNumUnits     = 400
)

func main() {
	_ = godotenv.Load()

	numUnits := defaultNumUnits
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil || n <= 0 {
			log.Fatalf("invalid unit count %q: must be a positive integer", os.Args[1])
		}
		numUnits = n
	}

	sdkKey := os.Getenv("LD_SDK_KEY")
	if sdkKey == "" {
		log.Fatal("LD_SDK_KEY is not set. Copy .env.example to .env and fill in your LaunchDarkly server-side SDK key.")
	}

	client, err := ld.MakeClient(sdkKey, 5*time.Second)
	if err != nil {
		log.Fatalf("failed to initialize LaunchDarkly client: %v", err)
	}
	defer client.Close()

	rand.Seed(time.Now().UnixNano())

	var legacyCount, runtimeCount int

	for i := 0; i < numUnits; i++ {
		key := fmt.Sprintf("synthetic-cluster-%04d", i)

		// Deterministically split contexts so half match the "production AND
		// enterprise" rule (-> runtime engine) and half don't (-> legacy
		// scanner via fallthrough). This guarantees a clean ~50/50 exposure
		// split regardless of the flag's real targeting config.
		var environment, plan string
		if i%2 == 0 {
			environment, plan = "production", "enterprise"
		} else {
			environments := []string{"staging", "development", "production"}
			plans := []string{"free", "team"} // never "enterprise", so Rule 2 can't match here
			environment = environments[rand.Intn(len(environments))]
			plan = plans[rand.Intn(len(plans))]
		}

		context := ldcontext.NewBuilder(key).
			Kind(ldcontexts.ContextKind).
			SetString("environment", environment).
			SetString("plan", plan).
			SetString("team", "synthetic-load-test").
			Build()

		usesRuntimeEngine, err := client.BoolVariation(flagKey, context, false)
		if err != nil {
			log.Printf("flag evaluation error for %s: %v", key, err)
			continue
		}

		// Skewed conversion rates matching the demo's premise: the runtime
		// engine produces fewer false positives and resolves more real
		// findings than the legacy scanner.
		falsePositiveRate := 0.40
		resolvedRate := 0.50
		if usesRuntimeEngine {
			runtimeCount++
			falsePositiveRate = 0.15
			resolvedRate = 0.70
		} else {
			legacyCount++
		}

		if rand.Float64() < falsePositiveRate {
			if err := client.TrackEvent(metricFalsePositive, context); err != nil {
				log.Printf("track event error for %s: %v", key, err)
			}
		}
		if rand.Float64() < resolvedRate {
			if err := client.TrackEvent(metricResolved, context); err != nil {
				log.Printf("track event error for %s: %v", key, err)
			}
		}
	}

	client.Flush()
	time.Sleep(3 * time.Second) // give the event processor time to deliver before Close flushes again

	fmt.Printf("seeded %d synthetic clusters (%d legacy, %d runtime engine)\n", numUnits, legacyCount, runtimeCount)
}
