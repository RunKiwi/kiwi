package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Generate creates a deterministic Manifest from a Job and an optional Workflow.
func Generate(job *store.Job, wf *store.Workflow) (*store.Manifest, error) {
	if job == nil {
		return nil, fmt.Errorf("job is required")
	}

	content := make(map[string]interface{})

	// Default to copying all inputs
	for k, v := range job.Inputs {
		content[k] = v
	}

	// TODO(P1.5): Pin provider/model/limits/egress/secrets once available in the Job model.
	// Currently serves as a single-agent stub.

	var workflowID *string
	if wf != nil {
		workflowID = &wf.ID
		// Merge workflow spec with job inputs
		for k, v := range wf.Spec {
			content[k] = v
		}
	}

	// Deterministic JSON serialization
	bytes, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest content: %w", err)
	}

	// Compute SHA256 of the JSON content
	hash := sha256.Sum256(bytes)
	manifestID := hex.EncodeToString(hash[:])

	return &store.Manifest{
		ID:            manifestID,
		OrgID:         job.OrgID,
		WorkflowID:    workflowID,
		SchemaVersion: "1.0",
		Content:       content,
		Producer:      "default",
		CreatedAt:     time.Now(),
	}, nil
}
