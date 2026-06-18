package api

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/support"
)

func TestProviderCapabilitiesComeFromRegistry(t *testing.T) {
	capabilities := capabilitiesFor(model.ProviderGitHub)
	if len(capabilities) == 0 {
		t.Fatal("expected registry-backed capabilities")
	}
	for _, capability := range capabilities {
		if _, ok := support.MustDefault().Get(capability.FeatureID); !ok {
			t.Fatalf("unknown capability id %s", capability.FeatureID)
		}
	}
}

func TestCompatibilityEventContainsSupportContract(t *testing.T) {
	feature, err := support.MustDefault().Resolve(model.FeatureRef{ID: "github.checkout", Path: "jobs.test.steps[0].uses"})
	if err != nil {
		t.Fatal(err)
	}
	event := compatibilityEvent(feature)
	for _, key := range []string{"featureId", "provider", "support", "runtimeDisposition", "localBehavior", "hostedDifferences", "securityImplications", "fallback", "documentation"} {
		if event.Data[key] == nil || event.Data[key] == "" {
			t.Fatalf("missing %s in %#v", key, event.Data)
		}
	}
}
