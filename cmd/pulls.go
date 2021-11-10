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
	"fmt"
	"github.com/spf13/cobra"
)

var (
	authors []string
	repo    string
)

// pullsCmd represents the pulls command
var pullsCmd = &cobra.Command{
	Use:   "pulls",
	Short: "Calculates average PR open time",
	Long: `Calculates the average time it takes for a pull request to get merged:

For the given repository, all closed PRs are fetched and the average
time is calculated by using created_at and merged_at fields
of each PR.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("pulls called")
		fmt.Println(authors, repo)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullsCmd)

	pullsCmd.Flags().StringArrayVarP(&authors, "authors", "a", []string{}, "authors to calculate PR open time")
	pullsCmd.MarkFlagRequired("toggle")

	pullsCmd.Flags().StringVarP(&repo, "repo", "r", "cockroach", "repository to fetch PRs from")
	pullsCmd.MarkFlagRequired("repo")
}
