package orchestrator

import (
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
	"gorm.io/gorm"
)

// recoverAction decides how to handle an interrupted task on boot: "relaunch" if
// its sandbox still exists on disk, otherwise "fail".
func recoverAction(sandboxPath *string) string {
	if sandboxPath == nil || *sandboxPath == "" {
		return "fail"
	}
	if _, err := os.Stat(*sandboxPath); err != nil {
		return "fail"
	}
	return "relaunch"
}

// RecoverTasks scans for tasks left RUNNING/PAUSED/PENDING by a previous daemon
// lifetime. Tasks whose sandbox survived are re-launched; the rest are failed.
func (s *Server) RecoverTasks() {
	var jobs []store.Job
	if err := s.db.Where("status IN ?", []string{"RUNNING", "PAUSED", "PENDING"}).Find(&jobs).Error; err != nil {
		return
	}
	for _, j := range jobs {
		if recoverAction(j.SandboxRef) == "relaunch" {
			tunnel.GlobalRegistry.Register(j.ID, j.UserID, j.OrgID)
			s.db.Model(&TaskState{}).Where("id = ?", j.ID).
				Update("logs", gorm.Expr("logs || ?", "\n[Recovery] Re-launched after daemon restart.\n"))
			
			task, _ := j.Inputs["task"].(string)
			file, _ := j.Inputs["file"].(string)
			testCmd, _ := j.Inputs["test_cmd"].(string)
			s.launchFn(j.ID, *j.SandboxRef, task, file, testCmd)
		} else {
			s.db.Model(&store.Job{}).Where("id = ?", j.ID).Update("status", "FAILED")
			s.db.Model(&TaskState{}).Where("id = ?", j.ID).Updates(map[string]interface{}{
				"status": "FAILED",
				"logs":   gorm.Expr("logs || ?", "\n[Recovery] Interrupted by daemon restart; sandbox unavailable.\n"),
			})
		}
	}
}
