package main

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/go-github/github"
	"github.com/nlopes/slack"
)

const slackMessageText string = ":ship: New release for [*<%s|%s>*] `%s`"

// Build struct
type Build struct {
	VcsTag    string   `json:"vcs_tag"`
	Workflows Workflow `json:"workflows"`
}

// Workflow struct
type Workflow struct {
	WorkflowID string `json:"workflow_id"`
}

func getCircleCIBuildURL(token, account, repo, tag string) (string, bool) {
	if token == "" {
		return "", false
	}

	url := fmt.Sprintf("https://circleci.com/api/v1.1/project/github/%s/%s?circle-token=%s", account, repo, token)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var circleBuilds []Build
	if err := json.Unmarshal(body, &circleBuilds); err != nil {
		return "", false
	}

	for _, build := range circleBuilds {
		if build.VcsTag == tag {
			return fmt.Sprintf("https://circleci.com/workflow-run/%s", build.Workflows.WorkflowID), true
		}
	}

	return "", false
}

func buildMessage(payload *github.ReleaseEvent) slack.Attachment {
	repo := strings.Split(payload.Repo.GetFullName(), "/")

	attachment := slack.Attachment{
		Title:      payload.Release.GetName(),
		AuthorName: payload.Release.Author.GetName(),
		AuthorIcon: payload.Release.Author.GetAvatarURL(),
		AuthorLink: payload.Release.Author.GetLogin(),
		Text:       payload.Release.GetBody(),
		Color:      "#4286f4",
		Ts:         json.Number(strconv.FormatInt(payload.Release.GetCreatedAt().Unix(), 10)),
		Fields: []slack.AttachmentField{
			{
				Title: "Tag",
				Value: payload.Release.GetHTMLURL(),
				Short: false,
			},
		},
	}

	if buildURL, ok := getCircleCIBuildURL(os.Getenv("CIRCLECI_TOKEN"), repo[0], repo[1], payload.Release.GetTagName()); ok {
		attachment.Fields = append(attachment.Fields, slack.AttachmentField{Title: "CircleCI", Value: buildURL, Short: false})
	}

	return attachment
}

// Handler is executed by AWS Lambda in the main function. Once the request
// is processed, it returns an Amazon API Gateway response object to AWS Lambda
func Handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	eventType := req.Headers["X-GitHub-Event"]
	if eventType != "release" {
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil
	}

	var payload github.ReleaseEvent
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Unable to handle request"}, err
	}

	if payload.Release.GetDraft() || payload.Release.GetPrerelease() {
		return events.APIGatewayProxyResponse{Body: `{ "done": true }`, StatusCode: 200}, nil
	}

	message := buildMessage(&payload)
	text := fmt.Sprintf(slackMessageText, payload.Repo.GetHTMLURL(), payload.Repo.GetName(), payload.Release.GetTagName())

	client := slack.New(req.PathParameters["token"])

	_, _, err := client.PostMessage(req.PathParameters["channel"],
		slack.MsgOptionText(text, false),
		slack.MsgOptionUsername("Release Bot"),
		slack.MsgOptionAttachments(message))

	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Unable to handle request"}, err
	}

	return events.APIGatewayProxyResponse{Body: `{ "done": true }`, StatusCode: 200}, nil
}

func main() {
	lambda.Start(Handler)
}
