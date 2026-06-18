package expression

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestEvaluateGitHubCondition(t *testing.T) {
	result := Evaluate(model.ConditionSpec{
		Provider: model.ProviderGitHub,
		Original: "${{ success() && github.ref == 'refs/heads/main' && startsWith(env.TAG, 'v') }}",
	}, Context{
		Status: model.RunSucceeded,
		Values: map[string]interface{}{
			"github": map[string]interface{}{"ref": "refs/heads/main"},
			"env":    map[string]interface{}{"TAG": "v1.2.3"},
		},
	})
	if result.Error != nil || !result.Value {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestEvaluateAzureFunctions(t *testing.T) {
	result := Evaluate(model.ConditionSpec{
		Provider: model.ProviderAzure,
		Original: "and(succeeded(), eq(variables.configuration, 'Release'))",
	}, Context{
		Status: model.RunSucceeded,
		Values: map[string]interface{}{
			"variables": map[string]interface{}{"configuration": "Release"},
		},
	})
	if result.Error != nil || !result.Value {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestInterpolate(t *testing.T) {
	got, err := Interpolate(model.ProviderGitHub, "echo ${{ matrix.os }}", Context{
		Values: map[string]interface{}{"matrix": map[string]interface{}{"os": "linux"}},
	})
	if err != nil || got != "echo linux" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestAzureBracketVariable(t *testing.T) {
	result := Evaluate(model.ConditionSpec{
		Provider: model.ProviderAzure,
		Original: "eq(variables['Build.SourceBranch'], 'refs/heads/main')",
	}, Context{Values: map[string]interface{}{
		"variables": map[string]interface{}{"Build.SourceBranch": "refs/heads/main"},
	}})
	if result.Error != nil || !result.Value {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestGitLabRegexComparison(t *testing.T) {
	result := Evaluate(model.ConditionSpec{
		Provider: model.ProviderGitLab,
		Original: `$CI_COMMIT_BRANCH =~ /^release-/`,
	}, Context{Values: map[string]interface{}{
		"env": map[string]interface{}{"CI_COMMIT_BRANCH": "release-1.0"},
	}})
	if result.Error != nil || !result.Value {
		t.Fatalf("unexpected result: %#v", result)
	}
}
