// Operator-only recovery. No HTTP server, jobs, or provider clients are started.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"mserp/internal/repository"
)

func main() {
	manifest := flag.String("manifest", "", "private reviewed identity JSON path")
	apply := flag.String("apply", "", "exact reviewed manifest SHA256; omitted previews with rollback")
	flag.Parse()
	if err := run(*manifest, *apply); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path, apply string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	if apply != "" && apply != digest {
		return fmt.Errorf("manifest SHA256 differs from reviewed preview")
	}
	var review repository.FleetScopeIdentityReview
	if err = json.Unmarshal(body, &review); err != nil {
		return err
	}
	_ = godotenv.Load(".env.local", ".env", "/etc/mserp/mserp.env")
	company := os.Getenv("FLEETSCOPE_COMPANY_ID")
	if company == "" || !strings.EqualFold(company, review.CompanyID) {
		return fmt.Errorf("manifest company does not match configured FleetScope company")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("database configuration invalid")
	}
	defer pool.Close()
	result, err := repository.NewFleetRepository(pool).ReviewFleetScopeIdentities(ctx, review, apply != "")
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		SHA256 string                                    `json:"sha256"`
		Result repository.FleetScopeIdentityReviewResult `json:"result"`
	}{digest, result})
}
