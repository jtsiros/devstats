# Devstats

[![forthebadge](https://forthebadge.com/images/badges/made-with-go.svg)](https://forthebadge.com)

[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square)](http://makeapullrequest.com)

A tool for groking Github PR statistics that gives users better visibility into PR velocity.

Calculates mean/median/median absolute deviation for the following:

- Merge time: how long it takes for a PR to be merged.
- Commits: number of commits per PR.
- Comments: number of comments per PR.
- Change size (+/-) : total number of line changes per PR.

Sum:
- PRs: total number of PRs merged by from date.

# Installation

`go install github.com/jtsiros/devstats@latest`

# Usage

`devstats pr [flags]`


  - [Flags](#flags)
    - `-a` or  `--author` stringArray   authors to calculate PR open time
    - `-f` or `--from`    string        from date to generate PR stats from
    - `-r` or `--repo`    string        repository to fetch PRs from (default "cockroachdb/cockroach")
    - `-h` or  `--help`                 help for pr
  - [Global](#global)
    -`--config`           string        config file (default is $HOME/.devstats.yaml)

# Configuration

`devstats` requires a Github personal access token. Instructions to generate one are [here](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token).

Once your token is created, you'll want to create your configuration file in the the default location ($HOME/.devstats.yaml), or provide a location of your choice using `--config`.

``` yaml
GITHUB_TOKEN: xxx_xxxxxxxxxx
```
