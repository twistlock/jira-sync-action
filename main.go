package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/andygrunwald/go-jira"
	"github.com/google/go-github/v33/github"
	"golang.org/x/oauth2"
)

// sync issues between GH to JIRA
func main() {
	if err := func() error {
		jiraConfig := &jiraConfig{
			url:      input("JIRA_URL"),
			user:     input("JIRA_USEREMAIL"),
			password: input("JIRA_APITOKEN"),
			project:  input("JIRA_PROJECTKEY"),
		}
		githubConfig, err := mkGithubConfig()
		if err != nil {
			return err
		}
		issue, err := fetchGithubIssue(githubConfig)
		if err != nil {
			return err
		}
		if err := syncIssue(jiraConfig, issue); err != nil {
			return fmt.Errorf("failed to sync issue %v", err)
		}
		return nil

	}(); err != nil {
		panic(err)
	}
}

// mkGithubConfig creates the github configuration
func mkGithubConfig() (*githubConfig, error) {
	// https://docs.github.com/en/actions/reference/environment-variables#default-environment-variables
	repo := input("GITHUB_REPOSITORY")
	tok := strings.SplitN(repo, "/", 2)
	if len(tok) != 2 {
		return nil, fmt.Errorf("invalid repo %v", repo)
	}

	v := input("GITHUB_ISSUE_NUMBER")
	if v == "" {
		return nil, fmt.Errorf("missing GH issue")
	}
	githubIssue, err := strconv.Atoi(v)
	if err != nil {
		return nil, err
	}

	return &githubConfig{
		token:   input("GITHUB_TOKEN"),
		owner:   tok[0],
		repo:    tok[1],
		issueID: githubIssue,
	}, nil
}

type githubConfig struct {
	token   string
	owner   string
	repo    string
	issueID int
}

func input(name string) string {
	for _, v := range os.Environ() {
		kv := strings.SplitN(v, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "INPUT_"+name || kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

type githubIssue struct {
	issue         *github.Issue
	comments      []*github.IssueComment
	nameByLoginID map[string]string // nameByLoginID maps login ID to display name
}

// fetchGithubIssue fetches Github issue and all relevant metadata
func fetchGithubIssue(conf *githubConfig) (*githubIssue, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: conf.token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Read issue metadata
	issue, _, err := client.Issues.Get(context.Background(), conf.owner, conf.repo, conf.issueID)
	if err != nil {
		return nil, err
	}
	res := &githubIssue{
		issue:         issue,
		nameByLoginID: make(map[string]string),
	}

	// Read user metadata
	user, _, err := client.Users.Get(context.Background(), stringify(issue.User.Login))
	if err != nil {
		return nil, err
	}
	res.nameByLoginID[stringify(issue.User.Login)] = stringify(user.Name)

	// Get all comments
	comments, _, err := client.Issues.ListComments(context.Background(), conf.owner, conf.repo, conf.issueID, &github.IssueListCommentsOptions{})
	if err != nil {
		return nil, err
	}
	res.comments = comments

	loginIDs := []*string{
		issue.User.Login,
	}
	// Aggregate all users and read their metadata
	if issue.Assignee != nil {
		loginIDs = append(loginIDs, issue.Assignee.Login)
	}

	for _, comment := range comments {
		loginIDs = append(loginIDs, comment.User.Login)
	}

	for _, loginID := range loginIDs {
		id := stringify(loginID)
		if _, ok := res.nameByLoginID[id]; ok {
			continue
		}
		user, _, err := client.Users.Get(context.Background(), id)
		if err != nil {
			return nil, err
		}
		res.nameByLoginID[id] = stringify(user.Name)
	}
	return res, nil
}

type jiraConfig struct {
	url      string
	user     string
	password string
	project  string
	state    string
}

func syncIssue(config *jiraConfig, githubIssue *githubIssue) error {
	tp := jira.BasicAuthTransport{
		Username: config.user,
		Password: config.password,
	}
	jiraClient, err := jira.NewClient(tp.Client(), config.url)
	if err != nil {

	}
	project, _, err := jiraClient.Project.Get(config.project)
	if err != nil {
		return err
	}

	// Try to find all users objects based on Github display name
	users := make(map[string]*jira.User)
	for loginID, name := range githubIssue.nameByLoginID {
		jiraUsers, resp, err := jiraClient.User.Find(url.QueryEscape(name))
		if err != nil {
			defer resp.Body.Close()
			body, _ := ioutil.ReadAll(resp.Body)
			return fmt.Errorf("failed to find user %s %w", string(body), err)
			return err
		}
		for i := range jiraUsers { // Ensure we got the correct user
			if strings.Contains(strings.ToLower(jiraUsers[i].DisplayName), strings.ToLower(name)) {
				users[loginID] = &jiraUsers[i]
				fmt.Println("asd", users[loginID])
				break
			}
		}
	}

	return nil

	// Create issue with the same title as GH issue
	title := stringify(githubIssue.issue.Title)
	// Remove `-` from query
	normalizedSummary := strings.Replace(title, "-", " ", -1)
	// Remove unsupported charts
	jql := fmt.Sprintf("project = %s AND summary ~ \"%s\"", project.Name, normalizedSummary)
	issues, _, err := jiraClient.Issue.Search(jql, nil)

	fmt.Printf("Found %d existing issues for jql: %s\n", len(issues), jql)

	if err != nil {
		return err
	}
	var fn func(issue *jira.Issue) (*jira.Issue, *jira.Response, error)

	var id int
	if githubIssue.issue.ID != nil {
		id = int(*githubIssue.issue.ID)
	}

	var assignee *jira.User
	// Set assignee
	if githubIssue.issue.Assignee != nil {
		assignee = users[stringify(githubIssue.issue.Assignee.Login)]
	}

	// https://docs.github.com/en/rest/reference/issues
	var labels []string

	// Add state as label, as it's not possible to close issues in JIRA
	// https://community.atlassian.com/t5/Jira-questions/No-close-issue-button/qaq-p/267132
	state := stringify(githubIssue.issue.State)
	if state != "" {
		labels = []string{state}
	}

	jiraIssue := jira.Issue{
		Key: strconv.Itoa(id),
		Fields: &jira.IssueFields{
			Project: *project,
			Labels:  labels,
			Type:    jira.IssueType{Name: "Task"},
			Summary: title,
			Description: stringify(githubIssue.issue.User.Login) +
				"\n" +
				stringify(githubIssue.issue.HTMLURL) +
				"\n" +
				stringify(githubIssue.issue.Body),
			Assignee: assignee,
		},
	}
	if len(issues) > 0 { // Update issue
		jiraIssue.Key = issues[0].Key
		fn = jiraClient.Issue.Update
		// Sync comments - delete than add all
		issueDetails, _, err := jiraClient.Issue.Get(issues[0].ID, nil)
		if err != nil {
			return err
		}
		for _, comment := range issueDetails.Fields.Comments.Comments {
			if comment != nil {
				if err := jiraClient.Issue.DeleteComment(issueDetails.ID, comment.ID); err != nil {
					return err
				}
			}
		}

		// Add all comments
		for _, comment := range githubIssue.comments {
			if _, _, err := jiraClient.Issue.AddComment(issueDetails.ID, &jira.Comment{
				// Add Github username
				Body: githubIssue.nameByLoginID[stringify(comment.User.Login)] + "\n" + stringify(comment.Body),
			}); err != nil {
				return err
			}
		}

		// Update issue state
		// https://docs.github.com/en/rest/reference/issues#list-issues-assigned-to-the-authenticated-user--parameters
		if state != "" {
			transitions, _, err := jiraClient.Issue.GetTransitions(issues[0].ID)
			if err != nil {
				return err
			}
			// https://docs.github.com/en/rest/reference/issues#list-issues-assigned-to-the-authenticated-user--parameters
			const closedState = "closed"
			for _, transition := range transitions {
				if (strings.Contains(strings.ToLower(transition.Name), "done") && state == closedState) ||
					(strings.Contains(strings.ToLower(transition.Name), "progress") && state != closedState) {
					if _, err := jiraClient.Issue.DoTransition(issues[0].ID, transition.ID); err != nil {
						return err
					}
					break
				}
			}
		}

	} else { // Create issue
		fn = jiraClient.Issue.Create
	}

	if _, resp, err := fn(&jiraIssue); err != nil {
		if resp != nil {
			defer resp.Body.Close()
			body, _ := ioutil.ReadAll(resp.Body)
			return fmt.Errorf("failed to set issue %s %w", body, err)
		}
		return err
	}
	return nil
}

func stringify(str *string) string {
	return strings.Trim(github.Stringify(str), "\"")
}
