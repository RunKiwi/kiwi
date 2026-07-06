package orchestrator

import (
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

// recoverAction decides how to handle an interrupted task on boot: "relaunch" if
// its sandbox still exists on disk, otherwise "fail".
func recoverAction(sandboxPath string) string {
	if sandboxPath == "" {
		return "fail"
	}
	if _, err := os.Stat(sandboxPath); err != nil {
		return "fail"
	}
	return "relaunch"
}

// RecoverTasks scans for tasks left RUNNING/PAUSED by a previous daemon
// lifetime. Tasks whose sandbox survived are re-launched; the rest are failed.
func (s *Server) RecoverTasks() {
	var tasks []TaskState
	if err := s.db.Where("status IN ?", []string{"RUNNING", "PAUSED"}).Find(&tasks).Error; err != nil {
		return
	}
	for _, t := range tasks {
		if recoverAction(t.SandboxPath) == "relaunch" {
			tunnel.GlobalRegistry.Register(t.ID)
			s.db.Model(&TaskState{}).Where("id = ?", t.ID).
				Update("logs", t.Logs+"\n[Recovery] Re-launched after daemon restart.\n")
			s.launchFn(t.ID, t.SandboxPath, t.Task, t.FilePath, t.TestCmd)
		} else {
			s.db.Model(&TaskState{}).Where("id = ?", t.ID).Updates(map[string]interface{}{
				"status": "FAILED",
				"logs":   t.Logs + "\n[Recovery] Interrupted by daemon restart; sandbox unavailable.\n",
			})
		}
	}
}
