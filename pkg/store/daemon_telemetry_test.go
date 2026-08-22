package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mustJoinToken(t *testing.T, s *PostgresStore, orgID string) string {
	t.Helper()
	token, err := s.CreateDaemonJoinToken(context.Background(), orgID, "", time.Hour)
	if err != nil {
		t.Fatalf("CreateDaemonJoinToken: %v", err)
	}
	return token
}

func TestUpdateDaemonTelemetry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkOrg(t, s, "org-1")

	signKey := mkSignKey(t)
	token := mustJoinToken(t, s, "org-1")
	d, err := s.RegisterDaemon(ctx, token, signKey, "enc-pub-1")
	require.NoError(t, err)

	// Update with cache stats and mem stats
	cacheStats := &CacheHeartbeatStats{
		TotalRepos:           18,
		TotalActiveWorktrees: 2,
		HitCount:             94,
		MissCount:            6,
	}
	memStats := []ContainerMemStats{
		{ContainerID: "c-01", RSSMB: 1024, LimitMB: 4096},
	}

	err = s.UpdateDaemonTelemetry(ctx, d.ID, cacheStats, memStats)
	require.NoError(t, err)

	got, err := s.GetDaemonBySignPubKey(ctx, signKey)
	require.NoError(t, err)
	require.NotNil(t, got.LastCacheStats)
	require.Equal(t, 18, got.LastCacheStats.TotalRepos)
	require.Equal(t, 2, got.LastCacheStats.TotalActiveWorktrees)
	require.Equal(t, int64(94), got.LastCacheStats.HitCount)
	require.Equal(t, int64(6), got.LastCacheStats.MissCount)
	require.Equal(t, 1, got.ActiveContainers)
	require.Len(t, got.LastMemStats, 1)
	require.Equal(t, "c-01", got.LastMemStats[0].ContainerID)
	require.Equal(t, int64(1024), got.LastMemStats[0].RSSMB)
	require.Equal(t, int64(4096), got.LastMemStats[0].LimitMB)

	// Calling UpdateDaemonTelemetry with nil cache and nil mem is a no-op and returns nil
	err = s.UpdateDaemonTelemetry(ctx, d.ID, nil, nil)
	require.NoError(t, err)

	// Daemon stats should remain unchanged
	gotAfterNil, err := s.GetDaemonBySignPubKey(ctx, signKey)
	require.NoError(t, err)
	require.NotNil(t, gotAfterNil.LastCacheStats)
	require.Equal(t, 18, gotAfterNil.LastCacheStats.TotalRepos)
	require.Equal(t, 1, gotAfterNil.ActiveContainers)
}
