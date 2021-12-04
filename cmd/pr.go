/*
Copyright © 2021 Jon Tsiros jon.tsiros@gmail.com

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/go-github/v40/github"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/montanaflynn/stats"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

func init() {
	rootCmd.AddCommand(prCmd)

	prCmd.Flags().StringArrayVarP(&prOpts.Authors, "author", "a", []string{}, "authors to calculate PR open time")
	prCmd.MarkFlagRequired("toggle")

	prCmd.Flags().StringVarP(&prOpts.Repo, "repo", "r", "cockroach", "repository to fetch PRs from")
	prCmd.MarkFlagRequired("repo")

	prCmd.Flags().StringVarP(&prOpts.FromDate, "from", "f", time.Now().AddDate(0, -1, 0).Format("2006-01-02"),
		"from date to generate PR stats. Defaults to past 30 days")
}

type SearchPROpts struct {
	FromDate string
	Authors  []string
	Repo     string
}

var (
	prOpts     = SearchPROpts{}
	nWorkers   = 4
	colorGreen = "\033[32m"
	colorRed   = "\033[31m"
)

// prCmd represents the pulls command
var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Calculates contributor statistics for all PRs contributed by an author(s).",
	Long: `Calculates mean/median/median absolute deviation for the following:

Merge time: how long it takes for a PR to be merged.
Commits: number of commits per PR.
Comments: number of comments per PR.
Change size (+/-) : total number of line changes per PR.

Sum:
PRs: total number of PRs merged by from date.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		viper.Unmarshal(&prOpts)
		return run()
	},
}

type ContributorStats struct {
	Comments   Statistics
	Commits    Statistics
	MergeTime  Statistics
	PRs        int
	ChangeSize Statistics
	Author     string
}

type Statistics struct {
	Mean                    float64
	Median                  float64
	MedianAbsoluteDeviation float64
}

func calcStats(s []float64) Statistics {
	if len(s) == 0 {
		return Statistics{0.0, 0.0, 0.0}
	}

	mR, _ := stats.Mean(s)
	medR, _ := stats.Median(s)
	madR, _ := stats.MedianAbsoluteDeviation(s)
	return Statistics{
		Mean:                    mR,
		Median:                  medR,
		MedianAbsoluteDeviation: madR,
	}
}

func run() error {
	ctx := context.Background()
	t := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: viper.GetString("GITHUB_TOKEN")},
	)
	oc := oauth2.NewClient(ctx, t)
	gc := github.NewClient(oc)

	stats := []ContributorStats{}
	fmt.Printf("Groking PR stats for %s from [%s]...", prOpts.Authors, prOpts.FromDate)

	for _, a := range prOpts.Authors {
		prs, err := pullRequests(ctx, gc, prOpts.Repo, a)
		if err != nil {
			return err
		}

		s := calculateStats(prs)
		s.Author = a
		stats = append(stats, s)
	}

	fmt.Println(colorGreen, "finished")
	render(stats)
	return nil
}

func pullRequests(ctx context.Context, gc *github.Client, repo string, author string) ([]*github.PullRequest, error) {

	ownerAndRepo := strings.Split(repo, "/")
	if len(ownerAndRepo) != 2 {
		return nil, errors.New("repo format error: must provide owner and repo. ex: jtsiros/devstats")
	}
	opt := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	g, ctx := errgroup.WithContext(ctx)
	issues := make(chan *github.Issue, 8)

	g.Go(func() error {
		defer close(issues)
		sba := searchByAuthor(author)
		for {
			sr, resp, err := gc.Search.Issues(ctx, sba, opt)
			if err != nil {
				return err
			}
			for _, i := range sr.Issues {
				issues <- i
			}

			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
		return nil
	})

	prs := make(chan *github.PullRequest, 8)
	workers := int32(nWorkers)

	for i := 0; i < nWorkers; i++ {
		g.Go(func() error {
			defer func() {
				if atomic.AddInt32(&workers, -1) == 0 {
					close(prs)
				}
			}()

			for i := range issues {
				pr, resp, err := gc.PullRequests.Get(ctx, ownerAndRepo[0], ownerAndRepo[1], i.GetNumber())
				if err != nil && resp.StatusCode != 404 {
					return fmt.Errorf("pr GET: %d - %v", i.GetNumber(), err)
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case prs <- pr:

				}
			}
			return nil
		})
	}

	// reduce
	allPRs := []*github.PullRequest{}
	for pr := range prs {
		allPRs = append(allPRs, pr)
	}
	return allPRs, g.Wait()
}

func calculateStats(prs []*github.PullRequest) ContributorStats {
	s := ContributorStats{}
	mergeDeltas := make([]float64, len(prs))
	commits := make([]float64, len(prs))
	comments := make([]float64, len(prs))
	changeSize := make([]float64, len(prs))

	for i, pr := range prs {
		delta := pr.GetMergedAt().Sub(pr.GetCreatedAt()).Hours()
		mergeDeltas[i] = delta
		changeSize[i] = float64(pr.GetAdditions() + pr.GetDeletions())
		commits[i] = float64(pr.GetCommits())
		comments[i] = float64(pr.GetComments())
	}

	s.MergeTime = calcStats(mergeDeltas)
	s.Commits = calcStats(commits)
	s.ChangeSize = calcStats(changeSize)
	s.Comments = calcStats(comments)
	s.PRs = len(prs)
	return s
}

func render(stats []ContributorStats) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"Author",
		"Merge Time (mean/median/mad)",
		"Comments (mean/median/mad)",
		"Commits (mean/median/mad)",
		"Change Size +/- (mean/median/mad)",
		"# of PRs",
	})

	var prs int
	for _, s := range stats {
		prs += s.PRs
		t.AppendRow(table.Row{
			s.Author,
			combined(s.MergeTime),
			combined(s.Comments),
			combined(s.Commits),
			combined(s.ChangeSize),
			s.PRs,
		})
	}
	t.AppendSeparator()
	t.SetStyle(table.StyleColoredBlackOnGreenWhite)
	t.Render()
}

func combined(s Statistics) string {
	return fmt.Sprintf("%s/%s/%s", shortFmt(s.Mean), shortFmt(s.Median), shortFmt(s.MedianAbsoluteDeviation))
}

func shortFmt(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

func searchByAuthor(author string) string {
	var sb strings.Builder
	sb.WriteString("is:pr is:closed is:merged")

	if len(prOpts.FromDate) != 0 {
		sb.WriteString(fmt.Sprintf(" created:>%s", prOpts.FromDate))
	}

	sb.WriteString(fmt.Sprintf(" author:%s", author))
	return sb.String()
}
