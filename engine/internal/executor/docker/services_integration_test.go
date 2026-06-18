//go:build integration

package docker

import (
	"context"
	"testing"

	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/secrets"
)

func TestPostgresAndRedisServices(t *testing.T) {
	ctx := context.Background()
	cli, err := connectDocker(ctx)
	if err != nil {
		t.Skip(err)
	}
	defer cli.Close()
	networkState, err := createJobNetwork(ctx, cli, "service-integration", false)
	if err != nil {
		t.Fatal(err)
	}
	defer networkState.cleanup(cli)
	services := []model.ServiceSpec{
		{
			Name: "postgres", Image: "postgres:16",
			Env:     map[string]string{"POSTGRES_PASSWORD": "piper", "POSTGRES_DB": "piper"},
			Aliases: []string{"postgres"}, StartupTimeout: 90,
		},
		{Name: "redis", Image: "redis:7", Aliases: []string{"redis"}, StartupTimeout: 60},
	}
	if err := startServices(ctx, cli, networkState, services, func(logs.Event) {}, secrets.NewMasker(nil)); err != nil {
		t.Fatal(err)
	}
	if len(networkState.containers) != 2 {
		t.Fatalf("containers = %d, want 2", len(networkState.containers))
	}
}
