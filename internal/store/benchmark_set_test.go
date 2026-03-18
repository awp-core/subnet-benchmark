package store

import (
	"context"
	"testing"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

func TestCreateBenchmarkSet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   model.BenchmarkSet
		wantErr bool
	}{
		{
			name: "basic creation",
			input: model.BenchmarkSet{
				SetID:                "bs_math",
				Description:          "Math Reasoning",
				QuestionRequirements: "unique answer",
				AnswerRequirements:   "integer",
				QuestionMaxLen:       1000,
				AnswerMaxLen:         500,
				AnswerCheckMethod:    "exact",
				Status:               model.BSStatusActive,
			},
		},
		{
			name: "duplicate set_id",
			input: model.BenchmarkSet{
				SetID:             "bs_math",
				Description:       "Duplicate",
				AnswerCheckMethod: "exact",
				Status:            model.BSStatusActive,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.CreateBenchmarkSet(ctx, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.SetID != tt.input.SetID {
				t.Errorf("SetID = %q, want %q", got.SetID, tt.input.SetID)
			}
			if got.Description != tt.input.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.input.Description)
			}
			if got.QuestionMaxLen != tt.input.QuestionMaxLen {
				t.Errorf("QuestionMaxLen = %d, want %d", got.QuestionMaxLen, tt.input.QuestionMaxLen)
			}
			if got.AnswerMaxLen != tt.input.AnswerMaxLen {
				t.Errorf("AnswerMaxLen = %d, want %d", got.AnswerMaxLen, tt.input.AnswerMaxLen)
			}
			if got.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}
		})
	}
}

func TestGetBenchmarkSet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, err := s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID:             "bs_code",
		Description:       "Coding",
		AnswerCheckMethod: "exact",
		Status:            model.BSStatusActive,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name    string
		setID   string
		wantNil bool
	}{
		{name: "existing", setID: "bs_code", wantNil: false},
		{name: "not found", setID: "bs_nonexist", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.GetBenchmarkSet(ctx, tt.setID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil, got nil")
			}
			if got.SetID != tt.setID {
				t.Errorf("SetID = %q, want %q", got.SetID, tt.setID)
			}
		})
	}
}

func TestListBenchmarkSets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Create 2 active + 1 inactive
	for _, bs := range []model.BenchmarkSet{
		{SetID: "bs_a", Description: "A", AnswerCheckMethod: "exact", Status: model.BSStatusActive},
		{SetID: "bs_b", Description: "B", AnswerCheckMethod: "exact", Status: model.BSStatusActive},
		{SetID: "bs_c", Description: "C", AnswerCheckMethod: "exact", Status: model.BSStatusInactive},
	} {
		if _, err := s.CreateBenchmarkSet(ctx, bs); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	tests := []struct {
		name   string
		status string
		want   int
	}{
		{name: "active only", status: model.BSStatusActive, want: 2},
		{name: "inactive only", status: model.BSStatusInactive, want: 1},
		{name: "all", status: "", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListBenchmarkSets(ctx, tt.status)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d items, want %d", len(got), tt.want)
			}
		})
	}
}

func TestUpdateBenchmarkSet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, err := s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID:             "bs_update",
		Description:       "Original",
		AnswerCheckMethod: "exact",
		Status:            model.BSStatusActive,
		QuestionMaxLen:    1000,
		AnswerMaxLen:      1000,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name    string
		setID   string
		fields  map[string]any
		check   func(t *testing.T, bs *model.BenchmarkSet)
		wantErr bool
	}{
		{
			name:  "update description",
			setID: "bs_update",
			fields: map[string]any{
				"description": "Updated",
			},
			check: func(t *testing.T, bs *model.BenchmarkSet) {
				if bs.Description != "Updated" {
					t.Errorf("Description = %q, want %q", bs.Description, "Updated")
				}
			},
		},
		{
			name:  "update status to inactive",
			setID: "bs_update",
			fields: map[string]any{
				"status": model.BSStatusInactive,
			},
			check: func(t *testing.T, bs *model.BenchmarkSet) {
				if bs.Status != model.BSStatusInactive {
					t.Errorf("Status = %q, want %q", bs.Status, model.BSStatusInactive)
				}
			},
		},
		{
			name:    "not found",
			setID:   "bs_nonexist",
			fields:  map[string]any{"description": "x"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.UpdateBenchmarkSet(ctx, tt.setID, tt.fields)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, got)
		})
	}
}
