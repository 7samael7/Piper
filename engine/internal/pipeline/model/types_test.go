package model

import "testing"

func TestCombineSupportUsesActionabilityOrder(t *testing.T) {
	tests := []struct {
		name   string
		levels []SupportLevel
		want   SupportLevel
	}{
		{name: "supported", levels: []SupportLevel{SupportSupportedLocal}, want: SupportSupportedLocal},
		{name: "emulated", levels: []SupportLevel{SupportSupportedLocal, SupportEmulated}, want: SupportEmulated},
		{name: "validation only", levels: []SupportLevel{SupportEmulated, SupportValidationOnly}, want: SupportValidationOnly},
		{name: "partial beats inspect only", levels: []SupportLevel{SupportValidationOnly, SupportPartial}, want: SupportPartial},
		{name: "consent beats partial", levels: []SupportLevel{SupportPartial, SupportRequiresConsent}, want: SupportRequiresConsent},
		{name: "unsupported wins", levels: []SupportLevel{SupportRequiresConsent, SupportUnsupported}, want: SupportUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CombineSupport(test.levels...); got != test.want {
				t.Fatalf("CombineSupport(%v) = %s, want %s", test.levels, got, test.want)
			}
		})
	}
}
