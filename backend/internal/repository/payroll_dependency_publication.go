package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

// Snapshot each exact shared key set once after canonical writes. Statement
// revisions stay local. Reuse ends with this transaction's publication phase.
func payrollPublicationDependencies(ctx context.Context, tx pgx.Tx, week string, resources map[string][]string) (map[string]json.RawMessage, error) {
	defer db.Phase(ctx, "publication_dependencies")()
	cache := map[string]json.RawMessage{}
	if phase, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState); phase != nil && phase.tx == tx {
		if phase.dependencySnapshots == nil {
			phase.dependencySnapshots = cache
		} else {
			cache = phase.dependencySnapshots
		}
	}
	groups := map[string][]string{}
	identities := map[string]string{}
	locals := []string{}
	for identity, keys := range resources {
		local := "statement:" + identity + ":" + week
		locals = append(locals, local)
		set := map[string]bool{}
		for _, key := range keys {
			if key != local {
				set[key] = true
			}
		}
		shared := make([]string, 0, len(set))
		for key := range set {
			shared = append(shared, key)
		}
		sort.Strings(shared)
		group := strings.Join(shared, ",")
		identities[identity] = group
		if _, ok := cache[group]; !ok {
			groups[group] = shared
		}
	}
	ordered := make([]string, 0, len(groups))
	for group := range groups {
		ordered = append(ordered, group)
	}
	sort.Strings(ordered)
	batch := &pgx.Batch{}
	for _, group := range ordered {
		batch.Queue(`SELECT payroll_pack_resource_snapshot($1::text[],'')`, groups[group])
	}
	sort.Strings(locals)
	batch.Queue(`SELECT payroll_resource_snapshot($1::text[])`, locals)
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	for _, group := range ordered {
		var value json.RawMessage
		if err := results.QueryRow().Scan(&value); err != nil {
			return nil, err
		}
		cache[group] = value
	}
	var body []byte
	if err := results.QueryRow().Scan(&body); err != nil {
		return nil, err
	}
	if err := results.Close(); err != nil {
		return nil, err
	}
	localRevisions := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &localRevisions); err != nil {
		return nil, err
	}
	packed := map[string]json.RawMessage{}
	for identity, group := range identities {
		value := map[string]json.RawMessage{}
		if err := json.Unmarshal(cache[group], &value); err != nil {
			return nil, err
		}
		local := "statement:" + identity + ":" + week
		revision, ok := localRevisions[local]
		if !ok {
			return nil, ErrPayrollResourceChanged
		}
		value[local] = revision
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		packed[identity] = encoded
	}
	return packed, nil
}
