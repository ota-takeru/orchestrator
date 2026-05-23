package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

type AppliedMigration struct {
	Version  int
	Checksum string
}

func NewMigration(version int, name string, sql string) Migration {
	sum := sha256.Sum256([]byte(sql))
	return Migration{
		Version:  version,
		Name:     name,
		SQL:      sql,
		Checksum: hex.EncodeToString(sum[:]),
	}
}

func ValidateMigrations(migrations []Migration) error {
	if len(migrations) == 0 {
		return fmt.Errorf("no migrations registered")
	}
	seen := map[int]struct{}{}
	previous := 0
	for _, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migration version must be positive: %d", migration.Version)
		}
		if migration.Version <= previous {
			return fmt.Errorf("migrations must be sorted by increasing version")
		}
		if _, ok := seen[migration.Version]; ok {
			return fmt.Errorf("duplicate migration version: %d", migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migration %d is missing name", migration.Version)
		}
		if migration.SQL == "" {
			return fmt.Errorf("migration %d is empty", migration.Version)
		}
		expected := NewMigration(migration.Version, migration.Name, migration.SQL).Checksum
		if migration.Checksum != expected {
			return fmt.Errorf("migration %d checksum mismatch in registry", migration.Version)
		}
		seen[migration.Version] = struct{}{}
		previous = migration.Version
	}
	return nil
}

func PlanMigrations(migrations []Migration, applied []AppliedMigration) ([]Migration, error) {
	if err := ValidateMigrations(migrations); err != nil {
		return nil, err
	}
	appliedByVersion := make(map[int]string, len(applied))
	for _, migration := range applied {
		appliedByVersion[migration.Version] = migration.Checksum
	}
	pending := make([]Migration, 0, len(migrations))
	for _, migration := range migrations {
		if checksum, ok := appliedByVersion[migration.Version]; ok {
			if checksum != migration.Checksum {
				return nil, fmt.Errorf("applied migration %d checksum changed", migration.Version)
			}
			continue
		}
		pending = append(pending, migration)
	}
	return pending, nil
}

func AppliedFromMap(applied map[int]string) []AppliedMigration {
	out := make([]AppliedMigration, 0, len(applied))
	for version, checksum := range applied {
		out = append(out, AppliedMigration{Version: version, Checksum: checksum})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version < out[j].Version
	})
	return out
}
