package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestListExecutionRecordsByOrgAndVer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mkRecord := func(jobID, kind string) {
		_, err := s.AppendExecutionRecord(ctx, "org-1", jobID, kind, func(prevHash string) (*ExecutionRecord, error) {
			body, _ := json.Marshal(map[string]string{"job_id": jobID})
			return &ExecutionRecord{
				RecordID: jobID + "-" + kind, OrgID: "org-1", JobID: jobID, Ver: kind,
				PrevRecordHash: prevHash, RecordHash: "h-" + jobID + kind, Body: body,
			}, nil
		})
		require.NoError(t, err)
	}
	mkRecord("job-exec-1", "kiwi.ver/v1")
	mkRecord("job-exec-1", "kiwi.ver/merge/v1") // same job, different kind — must be excluded
	mkRecord("job-exec-2", "kiwi.ver/v1")

	recs, err := s.ListExecutionRecordsByOrgAndVer(ctx, "org-1", "kiwi.ver/v1", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, recs, 2)
	for _, r := range recs {
		require.Equal(t, "kiwi.ver/v1", r.Ver)
	}
}
