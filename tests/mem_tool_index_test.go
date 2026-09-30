package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/llm"
)

func TestMemToolIndex_RanksByMatchedWords(t *testing.T) {
	ctx := context.Background()
	idx := agent.NewMemToolIndex()

	tools := []llm.ToolDef{
		{Name: "clinic_hours", Description: "opening hours of the clinic"},
		{Name: "book_appointment", Description: "book an appointment"},
		{Name: "weather", Description: "get weather forecast"},
	}

	if err := idx.IndexTools(ctx, tools); err != nil {
		t.Fatalf("IndexTools failed: %v", err)
	}

	got1, err := idx.SearchTools(ctx, "clinic opening hours", 5)
	if err != nil {
		t.Fatalf("SearchTools failed: %v", err)
	}
	if len(got1) != 1 || got1[0] != "clinic_hours" {
		t.Errorf("expected [clinic_hours], got %v", got1)
	}

	got2, err := idx.SearchTools(ctx, "book appointment clinic", 5)
	if err != nil {
		t.Fatalf("SearchTools failed: %v", err)
	}
	if len(got2) != 2 || got2[0] != "book_appointment" || got2[1] != "clinic_hours" {
		t.Errorf("expected [book_appointment, clinic_hours], got %v", got2)
	}
}

func TestMemToolIndex_LimitAndNoMatch(t *testing.T) {
	ctx := context.Background()
	idx := agent.NewMemToolIndex()

	tools := []llm.ToolDef{
		{Name: "clinic_hours", Description: "opening hours of the clinic"},
		{Name: "book_appointment", Description: "book an appointment"},
	}

	if err := idx.IndexTools(ctx, tools); err != nil {
		t.Fatalf("IndexTools failed: %v", err)
	}

	gotLimit, err := idx.SearchTools(ctx, "clinic appointment", 1)
	if err != nil {
		t.Fatalf("SearchTools failed: %v", err)
	}
	if len(gotLimit) != 1 {
		t.Errorf("expected 1 result with limit=1, got %d", len(gotLimit))
	}

	gotNone, err := idx.SearchTools(ctx, "zzz", 5)
	if err != nil {
		t.Fatalf("SearchTools failed: %v", err)
	}
	if len(gotNone) != 0 {
		t.Errorf("expected empty result for no match, got %v", gotNone)
	}
}
