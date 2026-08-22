// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleDaemonResultForwardsCacheTokenSplit(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	require.NoError(t, st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error)
	require.NoError(t, st.SaveCredential(ctx, "o1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-secret"))

	taskID := "task-cache-1"
	require.NoError(t, st.EnqueueTask(ctx, &store.QueuedTask{
		ID:     taskID,
		OrgID:  "o1",
		JobID:  "job-cache-1",
		Status: store.TaskQueued,
		Spec:   map[string]interface{}{"id": taskID, "task": "test cache tokens"},
	}))

	d := newDaemonKeys(t, ts.URL)
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	require.NoError(t, err)
	require.NoError(t, d.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
	}))

	res, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Specs, 1)
	require.NotEmpty(t, res.LeaseID)

	err = d.client.ReportResult(ctx, daemon.ResultReq{
		TaskID:             res.Specs[0].ID,
		LeaseID:            res.LeaseID,
		Status:             store.TaskSucceeded,
		SignPubKey:         d.signPubB64(),
		CachedPromptTokens: 800,
		RawPromptTokens:    200,
	})
	require.NoError(t, err)

	task, err := st.GetQueuedTask(ctx, taskID)
	require.NoError(t, err)
	require.Equal(t, int64(800), task.CachedPromptTokens)
	require.Equal(t, int64(200), task.RawPromptTokens)
}
