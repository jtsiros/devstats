/*
Copyright © 2021 Jon Tsiros jon.tsiros@brightblock.ai

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
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/montanaflynn/stats"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"

	"golang.org/x/sync/errgroup"
)

func init() {
	rootCmd.AddCommand(prCmd)

	prCmd.Flags().StringArrayVarP(&prOpts.Authors, "author", "a",
		[]string{}, "authors to calculate PR open time")

	_ = prCmd.MarkFlagRequired("toggle")

	prCmd.Flags().StringVarP(&prOpts.Repo, "repo", "r", "cockroach",
		"repository to fetch PRs from")

	_ = prCmd.MarkFlagRequired("repo")

	prCmd.Flags().StringVarP(&prOpts.FromDate, "from", "f",
		time.Now().AddDate(0, -1, 0).Format("2006-01-02"),
		"from date to generate PR stats. Defaults to past 30 days",
	)
}

type SearchPROpts struct {
	FromDate string
	Authors  []string
	Repo     string
}

const (
	colorGreen        = "\033[32m"
	nWorkers          = 4
	workerChanSize    = 1024
	ownerRepoTokenLen = 2
)

var (
	prOpts     = SearchPROpts{}
	errRepoFmt = errors.New("repo format error: must provide owner and repo. ex: jtsiros/devstats")
)

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
		return run()
	},
	SilenceUsage: true,
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
		return Statistics{}
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

	var cstats []ContributorStats
	fmt.Printf("Groking PR stats for %s from [%s]...\n", prOpts.Authors, prOpts.FromDate)

	for _, a := range prOpts.Authors {
		prs, err := pullRequests(ctx, gc, prOpts.Repo, a)
		if err != nil {
			return err
		}

		s := calculateStats(prs)
		s.Author = a
		cstats = append(cstats, s)
	}

	fmt.Println(colorGreen, "finished")
	render(cstats)

	return nil
}

// pullRequests fetches all PRs created by the author from a given date (default 30 days).
func pullRequests(ctx context.Context, gc *github.Client, repo string, author string) ([]*github.PullRequest, error) {
	ownerAndRepo := strings.Split(repo, "/")
	if len(ownerAndRepo) != ownerRepoTokenLen {
		return nil, errRepoFmt
	}

	opt := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	g, ctx := errgroup.WithContext(ctx)
	issues := make(chan *github.Issue, workerChanSize)

	g.Go(func() error {
		defer close(issues)
		sba := searchByAuthor(author, repo)

		fmt.Println("search query: [", sba, "]")
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

	prs := make(chan *github.PullRequest, workerChanSize)
	workers := int32(nWorkers)

	for i := 0; i < nWorkers; i++ {
		g.Go(func() error {
			defer func() {
				if atomic.AddInt32(&workers, -1) == 0 {
					close(prs)
				}
			}()

			for i := range issues {
				if !i.IsPullRequest() {
					continue
				}

				pr, resp, err := gc.PullRequests.Get(ctx,
					ownerAndRepo[0],
					ownerAndRepo[1],
					i.GetNumber(),
				)
				if err != nil {
					// skip processing this PR since we couldn't fetch it.
					continue
				}

				if resp.StatusCode != 200 {
					body, _ := io.ReadAll(resp.Body)
					err := resp.Body.Close()
					if err != nil {
						return err
					}

					return fmt.Errorf("PR GET (%d): [%d] - %s",
						resp.StatusCode,
						i.GetNumber(),
						body,
					)
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
	var allPRs []*github.PullRequest
	for pr := range prs {
		allPRs = append(allPRs, pr)
	}
	return allPRs, g.Wait()
}

func calculateStats(prs []*github.PullRequest) ContributorStats {
	mergeDeltas := make([]float64, len(prs))
	commits := make([]float64, len(prs))
	comments := make([]float64, len(prs))
	changeSize := make([]float64, len(prs))

	for i, pr := range prs {
		delta := pr.GetMergedAt().Sub(pr.GetCreatedAt().Time).Hours()
		mergeDeltas[i] = delta
		changeSize[i] = float64(pr.GetAdditions() + pr.GetDeletions())
		commits[i] = float64(pr.GetCommits())
		comments[i] = float64(pr.GetComments())
	}

	return ContributorStats{
		MergeTime:  calcStats(mergeDeltas),
		Commits:    calcStats(commits),
		ChangeSize: calcStats(changeSize),
		Comments:   calcStats(comments),
		PRs:        len(prs),
	}
}

func render(stats []ContributorStats) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"Author",
		"Merge Time (mean/median/mad) hours",
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
	return fmt.Sprintf("%s/%s/%s",
		shortFmt(s.Mean),
		shortFmt(s.Median),
		shortFmt(s.MedianAbsoluteDeviation))
}

func shortFmt(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

func searchByAuthor(author string, repo string) string {
	var sb strings.Builder

	sb.WriteString("is:pull-request is:closed is:merged")
	sb.WriteString(fmt.Sprintf(" repo:%s", repo))

	if len(prOpts.FromDate) != 0 {
		sb.WriteString(fmt.Sprintf(" created:>%s", prOpts.FromDate))
	}

	sb.WriteString(fmt.Sprintf(" author:%s", author))

	return sb.String()
}
