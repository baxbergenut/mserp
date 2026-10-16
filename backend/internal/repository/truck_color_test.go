package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckColorDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ string) {
		fleet := NewFleetRepository(pool)
		color := "White"
		input := TruckInput{UnitNumber: "COLOR-TEST", Color: &color, Active: true, Status: "available"}
		truck, err := fleet.CreateTruck(ctx, input)
		if err != nil || truck.Color == nil || *truck.Color != color {
			t.Fatal("create color", err)
		}
		input.Color = nil
		truck, err = fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil || truck.Color == nil || *truck.Color != "White" {
			t.Fatal("legacy update erased color", err)
		}
		color = "Blue"
		input.Color = &color
		truck, err = fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil || truck.Color == nil || *truck.Color != "Blue" {
			t.Fatal("update color", err)
		}
		color = ""
		truck, err = fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil || truck.Color != nil {
			t.Fatal("clear color", err)
		}
		color = strings.Repeat("a", 31)
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("database accepted overlong color")
		}
	})
}
