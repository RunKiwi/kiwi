package orchestrator

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type SpendBucket struct {
	Label    string  `json:"label"`
	ValueUSD float64 `json:"value_usd"`
}

type SpendPoint struct {
	Date       string  `json:"date"`
	PlannerUSD float64 `json:"planner_usd"`
	WorkerUSD  float64 `json:"worker_usd"`
}

type SpendResponse struct {
	TotalUSD     float64       `json:"total_usd"`
	JobCount     int           `json:"job_count"`
	MeteredJobs  int           `json:"metered_jobs"`
	Daily        []SpendPoint  `json:"daily"`
	ByRepository []SpendBucket `json:"by_repository"`
	ByModel      []SpendBucket `json:"by_model"`
}

func (s *Server) handleSpend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		to = time.Now()
	}
	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		from = to.Add(-30 * 24 * time.Hour)
	}
	
	if to.Sub(from) > 365*24*time.Hour {
		from = to.Add(-365 * 24 * time.Hour)
	}

	var jobs []store.Job
	if err := s.db.WithContext(r.Context()).Where("org_id = ? AND created_at >= ? AND created_at <= ?", claims.OrgID, from, to).Find(&jobs).Error; err != nil {
		http.Error(w, "failed to query jobs", http.StatusInternalServerError)
		return
	}

	var tasks []store.QueuedTask
	if err := s.db.WithContext(r.Context()).
		Joins("JOIN jobs ON jobs.id = queued_tasks.job_id").
		Where("jobs.org_id = ? AND jobs.created_at >= ? AND jobs.created_at <= ?", claims.OrgID, from, to).
		Find(&tasks).Error; err != nil {
		http.Error(w, "failed to query tasks", http.StatusInternalServerError)
		return
	}

	resp := SpendResponse{
		Daily:        make([]SpendPoint, 0),
		ByRepository: make([]SpendBucket, 0),
		ByModel:      make([]SpendBucket, 0),
	}
	
	dailyMap := make(map[string]*SpendPoint)
	days := int(to.Sub(from).Hours() / 24)
	if days == 0 {
		days = 1 // at least 1 day
	}
	for i := 0; i <= days; i++ {
		dateStr := from.Add(time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		dailyMap[dateStr] = &SpendPoint{Date: dateStr}
	}

	repoMap := make(map[string]float64)
	modelMap := make(map[string]float64)

	resp.JobCount = len(jobs)

	// Process jobs (Planner costs)
	for _, job := range jobs {
		totalCost := job.CostUSD + job.PlannerCostUSD
		resp.TotalUSD += totalCost
		if totalCost > 0 {
			resp.MeteredJobs++
		}
		
		dateStr := job.CreatedAt.Format("2006-01-02")
		if pt, ok := dailyMap[dateStr]; ok {
			pt.PlannerUSD += job.PlannerCostUSD
			// If this is a legacy job with no tasks but has worker cost
			if job.CostUSD > 0 {
				pt.WorkerUSD += job.CostUSD
			}
		}
		
		if job.Inputs != nil {
			if rUrl, ok := job.Inputs["repo_url"].(string); ok && rUrl != "" {
				repo := store.ShortRepo(rUrl)
				repoMap[repo] += job.PlannerCostUSD
			}
		}
	}

	// Process tasks (Worker costs)
	for _, t := range tasks {
		cost := t.CostUSD
		if cost == 0 {
			continue
		}
		
		repoUrl := specString(t.Spec, "repo_url")
		if repoUrl != "" {
			repo := store.ShortRepo(repoUrl)
			repoMap[repo] += cost
		}
		
		model := specString(t.Spec, "model")
		if model != "" {
			modelMap[model] += cost
		}
	}

	for _, pt := range dailyMap {
		resp.Daily = append(resp.Daily, *pt)
	}
	sort.Slice(resp.Daily, func(i, j int) bool {
		return resp.Daily[i].Date < resp.Daily[j].Date
	})

	for repo, val := range repoMap {
		resp.ByRepository = append(resp.ByRepository, SpendBucket{Label: repo, ValueUSD: val})
	}
	for model, val := range modelMap {
		resp.ByModel = append(resp.ByModel, SpendBucket{Label: model, ValueUSD: val})
	}

	sort.Slice(resp.ByRepository, func(i, j int) bool {
		return resp.ByRepository[i].ValueUSD > resp.ByRepository[j].ValueUSD
	})
	sort.Slice(resp.ByModel, func(i, j int) bool {
		return resp.ByModel[i].ValueUSD > resp.ByModel[j].ValueUSD
	})

	foldBuckets := func(buckets []SpendBucket) []SpendBucket {
		if len(buckets) <= 6 {
			return buckets
		}
		top := buckets[:6]
		var otherUSD float64
		for _, b := range buckets[6:] {
			otherUSD += b.ValueUSD
		}
		if otherUSD > 0 {
			top = append(top, SpendBucket{Label: "Other", ValueUSD: otherUSD})
		}
		return top
	}
	
	resp.ByRepository = foldBuckets(resp.ByRepository)
	resp.ByModel = foldBuckets(resp.ByModel)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
