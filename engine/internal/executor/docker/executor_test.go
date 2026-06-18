package docker

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/support"
)

func TestRuntimeGuardRejectsUnsupportedFeature(t *testing.T) {
	feature, rejected, err := rejectingFeature([]model.FeatureRef{{ID: "common.empty-step", Path: "jobs.test.steps[0]"}})
	if err != nil {
		t.Fatal(err)
	}
	if !rejected || feature.RuntimeDisposition != model.RuntimeReject {
		t.Fatalf("feature=%#v rejected=%v", feature, rejected)
	}
}

func TestRuntimeGuardMatchesEveryRegistryDisposition(t *testing.T) {
	for _, entry := range support.MustDefault().Features {
		_, rejected, err := rejectingFeature([]model.FeatureRef{{ID: entry.ID}})
		if err != nil {
			t.Fatalf("%s: %v", entry.ID, err)
		}
		wantRejected := entry.RuntimeDisposition == model.RuntimeReject
		if rejected != wantRejected {
			t.Fatalf("%s disposition=%s rejected=%v", entry.ID, entry.RuntimeDisposition, rejected)
		}
	}
}

func TestRuntimeGuardDoesNotRejectValidationOnlyOrConsent(t *testing.T) {
	for _, id := range []string{"github.runner", "github.remote-action"} {
		if _, rejected, err := rejectingFeature([]model.FeatureRef{{ID: id}}); err != nil || rejected {
			t.Fatalf("%s rejected=%v err=%v", id, rejected, err)
		}
	}
}

func TestCheckoutUsesEmulationEvent(t *testing.T) {
	event := emulationEvent("job", "step", "github.checkout", "emulated")
	if event.Type != "step_emulated" || event.Status != model.RunSucceeded {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.Data["featureId"] != "github.checkout" {
		t.Fatalf("unexpected data: %#v", event.Data)
	}
}
