package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type Build struct {
	VcsTag    string     `json:"vcs_tag"`
	Workflows []Workflow `json:"workflows"`
}

type Workflow struct {
	WorkflowId string `json:"workflow_id"`
}

func getCircleCIBuildUrl(token, account, repo string) (string, bool) {
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
			return build.Workflows[0].WorkflowId, true
		}
	}

	return "", false
}
