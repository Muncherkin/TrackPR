package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PR is an open pull request authored by the current user.
type PR struct {
	Number    int
	Title     string
	URL       string
	RepoNWO   string // repository nameWithOwner, e.g. "acme/web"
	CreatedAt time.Time
	UpdatedAt time.Time
	IsDraft   bool
}

type ghPR struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	IsDraft    bool      `json:"isDraft"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// FetchPRs returns every open pull request authored by the authenticated user
// across GitHub, via the gh CLI. Callers filter these down to local repos.
func FetchPRs() ([]PR, error) {
	cmd := exec.Command("gh", "search", "prs",
		"--author=@me", "--state=open",
		"--json", "number,title,url,createdAt,updatedAt,repository,isDraft",
		"--limit", "300",
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(ee.Stderr))
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("gh search prs: %s", msg)
		}
		return nil, fmt.Errorf("running gh (is it installed and authenticated?): %w", err)
	}

	var raw []ghPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}

	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		prs = append(prs, PR{
			Number:    r.Number,
			Title:     r.Title,
			URL:       r.URL,
			RepoNWO:   r.Repository.NameWithOwner,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			IsDraft:   r.IsDraft,
		})
	}
	return prs, nil
}
