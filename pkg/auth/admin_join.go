package auth

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

func handleListJoinRequests(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var reqs []OrgJoinRequest
	if err := db.Where("org_id = ? AND status = ?", orgID, "pending").Find(&reqs).Error; err != nil {
		http.Error(w, "Failed to list join requests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs)
}

func handleApproveJoinRequest(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, reqID string) {
	var req OrgJoinRequest
	if err := db.First(&req, "id = ? AND org_id = ?", reqID, orgID).Error; err != nil {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}

	if req.Status != "pending" {
		http.Error(w, "Request is not pending", http.StatusBadRequest)
		return
	}

	req.Status = "approved"
	if err := db.Save(&req).Error; err != nil {
		http.Error(w, "Failed to approve request", http.StatusInternalServerError)
		return
	}

	// Update the user's org to the company org
	var user User
	if err := db.First(&user, "email = ?", req.UserEmail).Error; err == nil {
		user.OrgID = orgID
		user.Role = "member"
		db.Save(&user)
	}

	w.WriteHeader(http.StatusOK)
}

func handleDenyJoinRequest(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, reqID string) {
	var req OrgJoinRequest
	if err := db.First(&req, "id = ? AND org_id = ?", reqID, orgID).Error; err != nil {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}

	if req.Status != "pending" {
		http.Error(w, "Request is not pending", http.StatusBadRequest)
		return
	}

	req.Status = "denied"
	if err := db.Save(&req).Error; err != nil {
		http.Error(w, "Failed to deny request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleToggleDomainJoin(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	var body struct {
		DomainJoin bool `json:"domain_join"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	org.DomainJoin = body.DomainJoin
	if err := db.Save(&org).Error; err != nil {
		http.Error(w, "Failed to update organization", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(org)
}
