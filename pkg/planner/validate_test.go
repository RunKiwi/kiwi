package planner

import (
	"strings"
	"testing"
)

func TestPlanValidate(t *testing.T) {
	tests := []struct {
		name       string
		plan       *Plan
		maxWorkers int
		wantErr    string
	}{
		{
			name: "valid plan",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go"},
					{ID: "w2", File: "b.go", DependsOn: []string{"w1"}},
				},
			},
			maxWorkers: 5,
		},
		{
			name: "exceeds max workers",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go"},
					{ID: "w2", File: "b.go"},
				},
			},
			maxWorkers: 1,
			wantErr:    "exceeds maximum workers limit",
		},
		{
			name: "duplicate worker id",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go"},
					{ID: "w1", File: "b.go"},
				},
			},
			maxWorkers: 5,
			wantErr:    "duplicate worker id",
		},
		{
			name: "unknown depends_on",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go", DependsOn: []string{"w3"}},
				},
			},
			maxWorkers: 5,
			wantErr:    "depends on unknown worker",
		},
		{
			name: "cycle detected",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go", DependsOn: []string{"w2"}},
					{ID: "w2", File: "b.go", DependsOn: []string{"w1"}},
				},
			},
			maxWorkers: 5,
			wantErr:    "contains a cycle",
		},
		{
			name: "overlapping files without dependency",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go"},
					{ID: "w2", File: "a.go"},
				},
			},
			maxWorkers: 5,
			wantErr:    "overlap on files but have no dependency path",
		},
		{
			name: "overlapping files with dependency",
			plan: &Plan{
				Workers: []PlannedWorker{
					{ID: "w1", File: "a.go"},
					{ID: "w2", File: "a.go", DependsOn: []string{"w1"}},
				},
			},
			maxWorkers: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.Validate(tt.maxWorkers)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
			}
		})
	}
}
