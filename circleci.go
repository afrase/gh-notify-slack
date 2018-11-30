package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// Build struct
type Build struct {
	VcsTag    string     `json:"vcs_tag"`
	Workflows []Workflow `json:"workflows"`
}

// Workflow struct
type Workflow struct {
	WorkflowID string `json:"workflow_id"`
}

func getCircleCIBuildURL(token, account, repo string) (string, bool) {
	if token == "" {
		return "", false
	}

	url := fmt.Sprintf("https://circleci.com/api/v1.1/project/github/%s/%s?token=%s", account, repo, token)
	resp, err := http.Get(url)
	if err != nil {
		return "", false
	}

	body, _ := ioutil.ReadAll(resp.Body)
	var circleBuilds []Build
	if err := json.Unmarshal(body, circleBuilds); err != nil {
		return "", false
	}

	for _, build := range circleBuilds {
		if build.VcsTag != "" {
			return build.Workflows[0].WorkflowID, true
		}
	}

	return "", false
}
