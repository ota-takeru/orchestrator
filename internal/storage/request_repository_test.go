package storage

import (
	"context"
	"testing"
)

func TestCreateFeatureRequestCreatesQueueItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	result, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して")
	if err != nil {
		t.Fatal(err)
	}
	if result.FeatureRequest.Status != "queued" || result.FeatureRequest.Source != "human" {
		t.Fatalf("feature request = %#v", result.FeatureRequest)
	}
	if result.QueueItem.ItemType != "feature_request_analysis" || result.QueueItem.ItemID != result.FeatureRequest.ID {
		t.Fatalf("queue item = %#v", result.QueueItem)
	}

	requests, err := db.ListFeatureRequests(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].ID != result.FeatureRequest.ID {
		t.Fatalf("requests = %#v", requests)
	}
	items, err := db.ListWorkQueueItems(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != result.QueueItem.ID {
		t.Fatalf("queue items = %#v", items)
	}
}

func TestCreateFeatureRequestAllowsDuplicateBody(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	first, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "同じ要望")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "同じ要望")
	if err != nil {
		t.Fatal(err)
	}
	if first.FeatureRequest.ID == second.FeatureRequest.ID {
		t.Fatalf("duplicate body reused id: %s", first.FeatureRequest.ID)
	}
}
