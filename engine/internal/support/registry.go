package support

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

//go:embed registry.json
var registryJSON []byte

type Entry struct {
	ID                   string                   `json:"id"`
	Provider             model.ProviderID         `json:"provider"`
	Category             string                   `json:"category"`
	Title                string                   `json:"title"`
	Status               model.SupportLevel       `json:"status"`
	ParserSupport        string                   `json:"parserSupport"`
	RuntimeDisposition   model.RuntimeDisposition `json:"runtimeDisposition"`
	LocalBehavior        string                   `json:"localBehavior"`
	HostedDifferences    string                   `json:"hostedDifferences"`
	SecurityImplications string                   `json:"securityImplications"`
	Fallback             string                   `json:"fallback"`
	RelatedTests         []string                 `json:"relatedTests"`
	Documentation        string                   `json:"documentation"`
}

type Registry struct {
	Features []Entry `json:"features"`
	byID     map[string]Entry
}

var (
	defaultRegistry *Registry
	defaultErr      error
	defaultOnce     sync.Once
)

func Default() (*Registry, error) {
	defaultOnce.Do(func() {
		defaultRegistry, defaultErr = Load(registryJSON)
	})
	return defaultRegistry, defaultErr
}

func MustDefault() *Registry {
	registry, err := Default()
	if err != nil {
		panic(err)
	}
	return registry
}

func Load(data []byte) (*Registry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var registry Registry
	if err := decoder.Decode(&registry); err != nil {
		return nil, fmt.Errorf("decode support registry: %w", err)
	}
	registry.byID = make(map[string]Entry, len(registry.Features))
	for index, entry := range registry.Features {
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("feature %d: %w", index, err)
		}
		if _, exists := registry.byID[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate feature id %q", entry.ID)
		}
		registry.byID[entry.ID] = entry
	}
	sort.Slice(registry.Features, func(i, j int) bool {
		if registry.Features[i].Provider != registry.Features[j].Provider {
			return registry.Features[i].Provider < registry.Features[j].Provider
		}
		if registry.Features[i].Category != registry.Features[j].Category {
			return registry.Features[i].Category < registry.Features[j].Category
		}
		return registry.Features[i].ID < registry.Features[j].ID
	})
	return &registry, nil
}

func validateEntry(entry Entry) error {
	required := map[string]string{
		"id": entry.ID, "provider": string(entry.Provider), "category": entry.Category,
		"title": entry.Title, "parserSupport": entry.ParserSupport,
		"localBehavior": entry.LocalBehavior, "hostedDifferences": entry.HostedDifferences,
		"securityImplications": entry.SecurityImplications, "fallback": entry.Fallback,
		"documentation": entry.Documentation,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !regexp.MustCompile(`^[a-z]+(?:[.-][a-z0-9]+)+$`).MatchString(entry.ID) {
		return fmt.Errorf("id %q must be a lowercase dotted feature identifier", entry.ID)
	}
	if !strings.HasPrefix(entry.ID, string(entry.Provider)+".") {
		return fmt.Errorf("id %q must start with provider %q", entry.ID, entry.Provider)
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`).MatchString(entry.Documentation) {
		return fmt.Errorf("documentation %q must be a lowercase anchor", entry.Documentation)
	}
	switch entry.Provider {
	case model.ProviderCommon, model.ProviderGitHub, model.ProviderGitLab, model.ProviderAzure:
	default:
		return fmt.Errorf("invalid provider %q", entry.Provider)
	}
	switch entry.Status {
	case model.SupportSupportedLocal, model.SupportEmulated, model.SupportPartial,
		model.SupportValidationOnly, model.SupportUnsupported, model.SupportRequiresConsent:
	default:
		return fmt.Errorf("invalid status %q", entry.Status)
	}
	switch entry.RuntimeDisposition {
	case model.RuntimeExecute, model.RuntimeEmulate, model.RuntimeInspectOnly, model.RuntimeReject, model.RuntimeConsent:
	default:
		return fmt.Errorf("invalid runtime disposition %q", entry.RuntimeDisposition)
	}
	if entry.Status == model.SupportUnsupported && entry.RuntimeDisposition != model.RuntimeReject {
		return fmt.Errorf("unsupported features must use reject disposition")
	}
	if entry.Status == model.SupportValidationOnly && entry.RuntimeDisposition != model.RuntimeInspectOnly {
		return fmt.Errorf("validation-only features must use inspect-only disposition")
	}
	if entry.Status == model.SupportRequiresConsent && entry.RuntimeDisposition != model.RuntimeConsent {
		return fmt.Errorf("requires-consent features must use consent disposition")
	}
	if entry.Status == model.SupportSupportedLocal && entry.RuntimeDisposition != model.RuntimeExecute {
		return fmt.Errorf("supported-local features must use execute disposition")
	}
	if entry.Status == model.SupportEmulated && entry.RuntimeDisposition != model.RuntimeEmulate {
		return fmt.Errorf("emulated features must use emulate disposition")
	}
	if entry.Status == model.SupportPartial && entry.RuntimeDisposition != model.RuntimeExecute {
		return fmt.Errorf("partial features must use execute disposition")
	}
	if len(entry.RelatedTests) == 0 {
		return fmt.Errorf("relatedTests must contain at least one test path")
	}
	return nil
}

func (r *Registry) Get(id string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	entry, ok := r.byID[id]
	return entry, ok
}

func (r *Registry) Resolve(ref model.FeatureRef) (model.FeatureSupport, error) {
	entry, ok := r.Get(ref.ID)
	if !ok {
		return model.FeatureSupport{}, fmt.Errorf("support registry has no entry for %q", ref.ID)
	}
	return model.FeatureSupport{
		FeatureID: entry.ID, Feature: entry.Title, Provider: entry.Provider, Category: entry.Category,
		Path: ref.Path, Origin: ref.Origin, Support: entry.Status, RuntimeDisposition: entry.RuntimeDisposition,
		Message: entry.LocalBehavior, LocalBehavior: entry.LocalBehavior, HostedDifferences: entry.HostedDifferences,
		SecurityImplications: entry.SecurityImplications, Fallback: entry.Fallback,
		Documentation: entry.Documentation,
	}, nil
}

func (r *Registry) EntriesFor(provider model.ProviderID) []Entry {
	result := []Entry{}
	for _, entry := range r.Features {
		if entry.Provider == provider || entry.Provider == model.ProviderCommon {
			result = append(result, entry)
		}
	}
	return result
}

func Ref(id, path string, origin *model.SourceOrigin) model.FeatureRef {
	return model.FeatureRef{ID: id, Path: path, Origin: origin}
}
