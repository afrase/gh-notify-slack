package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/go-github/github"
	"github.com/nlopes/slack"
)

const (
	slackMsgText         string = ":ship: New release for [*<%s|%s>*] `%s`"
	slackDisplayUsername string = "Release Bot"

	defaultFieldColor string = "36a64f"
)

// Build struct
type Build struct {
	VcsTag    string   `json:"vcs_tag"`
	Workflows Workflow `json:"workflows"`
}

// Workflow struct
type Workflow struct {
	WorkflowID string `json:"workflow_id"`
}

func getCircleCIBuilds(url string) ([]Build, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var circleBuilds []Build
	if err := json.Unmarshal(body, &circleBuilds); err != nil {
		return nil, err
	}

	return circleBuilds, nil
}

func getCircleCIBuildURL(token, account, repo, tag string) (string, bool) {
	if token == "" {
		return "", false
	}

	url := fmt.Sprintf("https://circleci.com/api/v1.1/project/github/%s/%s?circle-token=%s", account, repo, token)
	// try 3 times to find the build url
	for i := 0; i < 2; i++ {
		builds, err := getCircleCIBuilds(url)
		if err != nil {
			break
		}

		for _, build := range builds {
			if build.VcsTag == tag {
				return fmt.Sprintf("https://circleci.com/workflow-run/%s", build.Workflows.WorkflowID), true
			}
		}
		time.Sleep(1 * time.Second)
	}

	return "", false
}

func buildAttachment(payload *github.ReleaseEvent, color string) slack.Attachment {
	attachment := slack.Attachment{
		Title:      payload.Release.GetName(),
		AuthorName: payload.Release.Author.GetLogin(),
		AuthorIcon: payload.Release.Author.GetAvatarURL(),
		AuthorLink: payload.Release.Author.GetHTMLURL(),
		Text:       payload.Release.GetBody(),
		Color:      fmt.Sprintf("#%s", color),
		Ts:         json.Number(fmt.Sprintf("%d", payload.Release.GetPublishedAt().Unix())),
		Fields: []slack.AttachmentField{
			{
				Title: "Tag",
				Value: payload.Release.GetHTMLURL(),
				Short: false,
			},
		},
	}

	repo := strings.Split(payload.Repo.GetFullName(), "/")
	buildURL, ok := getCircleCIBuildURL(os.Getenv("CIRCLECI_TOKEN"), repo[0], repo[1], payload.Release.GetTagName())
	// add the CircleCI link if it exists
	if ok {
		attachment.Fields = append(attachment.Fields, slack.AttachmentField{Title: "CircleCI", Value: buildURL, Short: false})
	}

	return attachment
}

func sendSlackMessage(event *github.ReleaseEvent, token, channel, color string) error {
	attachment := buildAttachment(event, color)
	username := slack.MsgOptionUsername(slackDisplayUsername)
	text := fmt.Sprintf(slackMsgText, event.Repo.GetHTMLURL(), event.Repo.GetName(), event.Release.GetTagName())
	message := slack.MsgOptionText(text, false)
	client := slack.New(token)

	_, _, err := client.PostMessage(channel, message, username, slack.MsgOptionAttachments(attachment))
	return err
}

// Handler is executed by AWS Lambda in the main function. Once the request
// is processed, it returns an Amazon API Gateway response object to AWS Lambda
func Handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	eventType := req.Headers["X-GitHub-Event"]
	// we only care about release events
	if eventType != "release" {
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil
	}

	var payload github.ReleaseEvent
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Unable to handle request"}, err
	}

	color, ok := req.QueryStringParameters["color"]
	if !ok {
		color = defaultFieldColor
	}
	err := sendSlackMessage(&payload, req.PathParameters["token"], req.PathParameters["channel"], color)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to send slack message"}, err
	}

	return events.APIGatewayProxyResponse{Body: `{ "done": true }`, StatusCode: 200}, nil
}

func main() {
	lambda.Start(Handler)
}
