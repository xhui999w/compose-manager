package app

import (
	"testing"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/model"
)

func TestFilterNestedProjectsHidesOrphanSnapshots(t *testing.T) {
	files := []compose.DiscoveredProject{
		{Key: "iyuu", Name: "iyuu"},
		{Key: "docker", Name: "docker", Nested: true},
		{Key: "nested-live", Name: "nested-live", Nested: true},
	}
	containers := []model.Container{{Project: "nested-live"}}
	filtered := filterNestedProjects(files, containers)
	if len(filtered) != 2 {
		t.Fatalf("got %d projects, want 2: %+v", len(filtered), filtered)
	}
	for _, project := range filtered {
		if project.Key == "docker" {
			t.Fatal("orphan nested project was not filtered")
		}
	}
}

func TestFilterNestedProjectsPrefersTopLevelDuplicate(t *testing.T) {
	files := []compose.DiscoveredProject{
		{Key: "silnav", Name: "silnav"},
		{Key: "silnav", Name: "silnav snapshot", Nested: true},
	}
	filtered := filterNestedProjects(files, []model.Container{{Project: "silnav"}})
	if len(filtered) != 1 || filtered[0].Name != "silnav" {
		t.Fatalf("top-level project was not preferred: %+v", filtered)
	}
}
