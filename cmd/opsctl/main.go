package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/template"
)

var tfvarsTemplate = `project_id   = "{{.ProjectID}}"
region       = "{{.Region}}"
zone         = "{{.Zone}}"
org_id       = "{{.OrgID}}"
join_token   = "{{.JoinToken}}"
api_url      = "{{.APIURL}}"
daemon_image = "{{.DaemonImage}}"
`

type Config struct {
	ProjectID   string
	Region      string
	Zone        string
	OrgID       string
	JoinToken   string
	APIURL      string
	DaemonImage string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("opsctl provision-daemon", flag.ContinueOnError)
	orgID := fs.String("org-id", "", "The organization ID")
	apiURL := fs.String("api-url", "https://api.runkiwi.com", "The Kiwi API URL")
	daemonImage := fs.String("image", "us-central1-docker.pkg.dev/my-gcp-project-id/kiwi-repo/kiwidaemon:latest", "The kiwidaemon image")
	projectID := fs.String("project", "my-gcp-project-id", "GCP Project ID")
	region := fs.String("region", "us-central1", "GCP Region")
	zone := fs.String("zone", "us-central1-a", "GCP Zone")
	token := fs.String("server-token", os.Getenv("KIWI_SERVER_TOKEN"), "The admin server token for authentication")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *orgID == "" {
		return fmt.Errorf("-org-id is required")
	}

	joinToken, err := mintJoinToken(*apiURL, *orgID, *token)
	if err != nil {
		return fmt.Errorf("failed to mint join token: %w", err)
	}

	cfg := Config{
		ProjectID:   *projectID,
		Region:      *region,
		Zone:        *zone,
		OrgID:       *orgID,
		JoinToken:   joinToken,
		APIURL:      *apiURL,
		DaemonImage: *daemonImage,
	}

	tmpl, err := template.New("tfvars").Parse(tfvarsTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	if err := tmpl.Execute(out, cfg); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

func mintJoinToken(apiURL, orgID, serverToken string) (string, error) {
	reqBody, _ := json.Marshal(map[string]string{"org_id": orgID})
	req, err := http.NewRequest(http.MethodPost, apiURL+"/api/v1/daemon/join-token", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if serverToken != "" {
		req.Header.Set("Authorization", "Bearer "+serverToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	if data.Token == "" {
		return "", fmt.Errorf("API returned empty token")
	}

	return data.Token, nil
}
