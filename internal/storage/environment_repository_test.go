package storage

import (
	"context"
	"testing"
)

func TestListExecutionEnvironments(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	envs, err := db.ListExecutionEnvironments(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].ID != "linux-main" {
		t.Fatalf("envs = %#v", envs)
	}
}
