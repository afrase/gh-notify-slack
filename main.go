package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/go-github/github"
	"github.com/nlopes/slack"
)

const (
	slackProdMsgText     string = ":ship: New Production release for [*<%s|%s>*] `%s`"
	slackStageMsgText    string = ":construction: New Stage release for [*<%s|%s>*] `%s`"
	slackDisplayUsername string = "Release Bot"

	defaultFieldColor string = "36a64f"

	circleCIProjectURL  = "https://circleci.com/api/v1.1/project/github/%s/%s?circle-token=%s"
	circleCIWorkflowURL = "https://circleci.com/workflow-run/%s"
)

var (
	pullRequestRegexp = regexp.MustCompile(`#(\d+)`)
)

// Build struct for CircleCI response
type Build struct {
	VcsTag    string   `json:"vcs_tag"`
	Workflows Workflow `json:"workflows"`
}

// Workflow struct for CircleCI response
type Workflow struct {
	WorkflowID string `json:"workflow_id"`
}

func getCircleCIBuilds(url string) ([]Build, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	// Set a 60 second timeout.
	client := &http.Client{Timeout: time.Second * 60}
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

	url := fmt.Sprintf(circleCIProjectURL, account, repo, token)
	// try 4 times to find the build url
	for i := 1; i < 4; i++ {
		builds, err := getCircleCIBuilds(url)
		if err != nil {
			break
		}

		for _, build := range builds {
			if build.VcsTag == tag {
				return fmt.Sprintf(circleCIWorkflowURL, build.Workflows.WorkflowID), true
			}
		}
		time.Sleep(time.Duration(i*2) * time.Second)
	}

	return "", false
}

func parseReleaseBody(payload *github.ReleaseEvent) string {
	pullRequestURL := fmt.Sprintf("<%s/pull/$1|#$1>", payload.Repo.GetHTMLURL())
	return pullRequestRegexp.ReplaceAllString(payload.Release.GetBody(), pullRequestURL)
}

func buildAttachment(payload *github.ReleaseEvent, color string) slack.Attachment {
	// If the author was a bot then use the sender which is the person who publishes the release.
	var user *github.User
	if payload.Release.Author.GetType() == "Bot" {
		user = payload.Sender
	} else {
		user = payload.Release.Author
	}

	attachment := slack.Attachment{
		Title:      payload.Release.GetName(),
		AuthorName: user.GetLogin(),
		AuthorIcon: user.GetAvatarURL(),
		AuthorLink: user.GetHTMLURL(),
		Text:       parseReleaseBody(payload),
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
	var text string
	if strings.Contains(event.Release.GetTagName(), "-rc") {
		text = fmt.Sprintf(slackStageMsgText, event.Repo.GetHTMLURL(), event.Repo.GetName(), event.Release.GetTagName())
	} else {
		text = fmt.Sprintf(slackProdMsgText, event.Repo.GetHTMLURL(), event.Repo.GetName(), event.Release.GetTagName())
	}
	message := slack.MsgOptionText(text, false)
	client := slack.New(token)

	_, _, err := client.PostMessage(channel, message, username, slack.MsgOptionAttachments(attachment))
	return err
}

// Handler is executed by AWS Lambda in the main function. Once the request
// is processed, it returns an Amazon API Gateway response object to AWS Lambda
func Handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Check the headers first because the body might not be in a format we expect.
	eventType := req.Headers["X-GitHub-Event"]
	// we only care about release events.
	if eventType != "release" {
		return events.APIGatewayProxyResponse{Body: "Skipping because event is not 'release'", StatusCode: 200}, nil
	}

	// Parse the body into a `github.ReleaseEvent` struct.
	var payload github.ReleaseEvent
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Unable to handle request"}, err
	}

	// New actions have been added to the 'release' event. Only look for the 'publish' action.
	if *payload.Action != "published" {
		return events.APIGatewayProxyResponse{Body: "Skipping because action is not 'published'", StatusCode: 200}, nil
	}

	// Get the color query param if it exists.
	color, ok := req.QueryStringParameters["color"]
	if !ok {
		color = defaultFieldColor
	}
	err := sendSlackMessage(&payload, req.PathParameters["token"], req.PathParameters["channel"], color)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Failed to send slack message"}, err
	}

	return events.APIGatewayProxyResponse{Body: "Success", StatusCode: 200}, nil
}

func main() {
	lambda.Start(Handler)
}
