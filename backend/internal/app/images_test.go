package app

import (
	"testing"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

func TestNormalizeImageReferences(t *testing.T) {
	image := model.ImageReference{}
	normalizeImageReferences(&image)
	if image.RunningReferences == nil || image.StoppedReferences == nil || image.ComposeReferences == nil {
		t.Fatal("image reference collections must serialize as empty arrays, not null")
	}
}

func TestClassifyImage(t *testing.T) {
	tests := []struct {
		name         string
		image        model.ImageReference
		wantCategory string
		wantReclaim  uint64
	}{
		{name: "running", image: model.ImageReference{RunningReferences: []string{"app"}}, wantCategory: "in-use"},
		{name: "compose", image: model.ImageReference{ComposeReferences: []string{"stack"}}, wantCategory: "compose-referenced"},
		{name: "stopped", image: model.ImageReference{StoppedReferences: []string{"old"}}, wantCategory: "old-version"},
		{name: "unused", image: model.ImageReference{Repository: "example/app", Size: 100}, wantCategory: "unused", wantReclaim: 100},
		{name: "dangling", image: model.ImageReference{Repository: "<none>", Size: 200}, wantCategory: "dangling", wantReclaim: 200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classifyImage(&test.image)
			if test.image.Category != test.wantCategory || test.image.Reclaimable != test.wantReclaim {
				t.Fatalf("classifyImage() = category %q, reclaimable %d; want %q, %d", test.image.Category, test.image.Reclaimable, test.wantCategory, test.wantReclaim)
			}
		})
	}
}
