// Copyright 2016 Documize Inc. <legal@documize.com>. All rights reserved.
//
// This software (Documize Community Edition) is licensed under
// GNU AGPL v3 http://www.gnu.org/licenses/agpl-3.0.en.html
//
// You can operate outside the AGPL restrictions by purchasing
// Documize Enterprise Edition and obtaining a commercial license
// by contacting <sales@documize.com>.
//
// https://documize.com

package github

import (
	"html/template"
	"sort"
	"time"

	"github.com/documize/community/core/log"

	gogithub "github.com/google/go-github/github"
)

type githubIssue struct {
	ID         int           `json:"id"`
	Date       string        `json:"date"`
	Updated    string        `json:"dated"`
	Message    string        `json:"message"`
	URL        template.URL  `json:"url"`
	Name       string        `json:"name"`
	Avatar     string        `json:"avatar"`
	Labels     template.HTML `json:"labels"`
	LabelNames []string      `json:"labelNames"`
	IsOpen     bool          `json:"isopen"`
	Repo       string        `json:"repo"`
	Private    bool          `json:"private"`
	Milestone  string        `json:"milestone"`
}

type githubSharedLabel struct {
	Name  string        `json:"name"`
	Count int           `json:"count"`
	Repos template.HTML `json:"Repos"`
}

// sort issues in order that that should be presented - by date updated.
type issuesToSort []githubIssue

func (s issuesToSort) Len() int      { return len(s) }
func (s issuesToSort) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s issuesToSort) Less(i, j int) bool {
	if s[i].Milestone != noMilestone && s[j].Milestone == noMilestone {
		return true
	}
	if s[i].Milestone == noMilestone && s[j].Milestone != noMilestone {
		return false
	}
	if s[i].Milestone != s[j].Milestone {
		// TODO should this order be by milestone completion?
		return s[i].Milestone < s[j].Milestone
	}
	if !s[i].IsOpen && s[j].IsOpen {
		return true
	}
	if s[i].IsOpen && !s[j].IsOpen {
		return false
	}
	// TODO this seems a very slow approach
	iDate, iErr := time.Parse(issuesTimeFormat, s[i].Updated)
	log.IfErr(iErr)
	jDate, jErr := time.Parse(issuesTimeFormat, s[j].Updated)
	log.IfErr(jErr)
	return iDate.Before(jDate)
}

// sort shared labels alphabetically
type sharedLabelsSort []githubSharedLabel

func (s sharedLabelsSort) Len() int           { return len(s) }
func (s sharedLabelsSort) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s sharedLabelsSort) Less(i, j int) bool { return s[i].Name < s[j].Name }

const (
	tagIssuesData    = "issuesData"
	issuesTimeFormat = "January 2 2006, 15:04"
)

func init() {
	reports[tagIssuesData] = report{refreshIssues, renderIssues, issuesTemplate}
}

func wrapLabels(labels []gogithub.Label) (l string, labelNames []string) {
	labelNames = make([]string, 0, len(labels))
	for _, ll := range labels {
		labelNames = append(labelNames, *ll.Name)
		l += `<span class="github-issue-label" style="background-color:#` + *ll.Color + `">` + *ll.Name + `</span> `
	}
	return l, labelNames
}

func getIssues(client *gogithub.Client, config *githubConfig) ([]githubIssue, error) {

	ret := []githubIssue{}

	hadRepo := make(map[string]bool)

	for _, orb := range config.Lists {
		if orb.Included {

			rName := orb.Owner + "/" + orb.Repo

			if !hadRepo[rName] {

				for _, state := range []string{"open", "closed"} {

					opts := &gogithub.IssueListByRepoOptions{
						Sort:        "updated",
						State:       state,
						ListOptions: gogithub.ListOptions{PerPage: config.BranchLines}}

					if config.SincePtr != nil && state == "closed" /* we want all the open ones */ {
						opts.Since = *config.SincePtr
					}

					guff, _, err := client.Issues.ListByRepo(orb.Owner, orb.Repo, opts)

					if err != nil {
						return ret, err
					}

					for _, v := range guff {
						n := "(unassigned)"
						av := githubGravatar
						ptr := v.Assignee
						if ptr != nil {
							if ptr.Login != nil {
								n = *ptr.Login
								av = *ptr.AvatarURL
							}
						}
						ms := noMilestone
						if v.Milestone != nil {
							if v.Milestone.Title != nil {
								ms = *v.Milestone.Title
							}
						}
						l, ln := wrapLabels(v.Labels)
						ret = append(ret, githubIssue{
							Name:       n,
							Avatar:     av,
							Message:    *v.Title,
							Date:       v.CreatedAt.Format(issuesTimeFormat),
							Updated:    v.UpdatedAt.Format(issuesTimeFormat),
							URL:        template.URL(*v.HTMLURL),
							Labels:     template.HTML(l),
							LabelNames: ln,
							ID:         *v.Number,
							IsOpen:     *v.State == "open",
							Repo:       repoName(rName),
							Private:    orb.Private,
							Milestone:  ms,
						})
					}
				}
			}
			hadRepo[rName] = true
		}

	}

	sort.Sort(issuesToSort(ret))

	return ret, nil

}

func refreshIssues(gr *githubRender, config *githubConfig, client *gogithub.Client) (err error) {
	gr.Issues, err = getIssues(client, config)
	if err != nil {
		log.Error("unable to get github issues (cmd)", err)
		return err
	}

	gr.OpenIssues = 0
	gr.ClosedIssues = 0
	sharedLabels := make(map[string][]string)
	for _, v := range gr.Issues {
		if v.IsOpen {
			gr.OpenIssues++
		} else {
			gr.ClosedIssues++
		}
		for _, lab := range v.LabelNames {
			sharedLabels[lab] = append(sharedLabels[lab], v.Repo)
		}
	}
	gr.HasIssues = (gr.OpenIssues + gr.ClosedIssues) > 0

	gr.SharedLabels = make([]githubSharedLabel, 0, len(sharedLabels)) // will usually be too big
	for name, repos := range sharedLabels {
		if len(repos) > 1 {
			thisLab := githubSharedLabel{Name: name, Count: len(repos)}
			show := ""
			for i, r := range repos {
				if i > 0 {
					show += ", "
				}
				show += "<a href='https://github.com/" + config.Owner + "/" + r +
					"/issues?q=is%3Aissue+label%3A" + name + "'>" + r + "</a>"
			}
			thisLab.Repos = template.HTML(show)
			gr.SharedLabels = append(gr.SharedLabels, thisLab)
		}
	}
	sort.Sort(sharedLabelsSort(gr.SharedLabels))
	gr.HasSharedLabels = len(gr.SharedLabels) > 0

	return nil
}

func renderIssues(payload *githubRender, c *githubConfig) error {
	return nil
}
