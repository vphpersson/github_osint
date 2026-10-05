package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	argument_parser "github.com/altshiftab/utils_go/pkg/cli/argument_parser"
	argumentParserErrors "github.com/altshiftab/utils_go/pkg/cli/argument_parser/errors"
	"github.com/altshiftab/utils_go/pkg/cli/argument_parser/option"
	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	altshiftHttpErrors "github.com/altshiftab/utils_go/pkg/http/errors"
	"github.com/altshiftab/utils_go/pkg/http/types/fetch_config"
	"github.com/altshiftab/utils_go/pkg/http/types/fetch_config/retry_config"
	altshiftLog "github.com/altshiftab/utils_go/pkg/log"
	altshiftErrorLogger "github.com/altshiftab/utils_go/pkg/log/error_logger"

	"github.com/Motmedel/github_osint/pkg/github"
	"github.com/Motmedel/github_osint/pkg/github/github_config"
	"github.com/Motmedel/github_osint/pkg/github/types/commit"
	"github.com/Motmedel/github_osint/pkg/github/types/repository"
)

type CommitAuthor struct {
	Name         string
	EmailAddress string
}

func (a CommitAuthor) String() string {
	return a.EmailAddress + " " + a.Name
}

type RepositoryInfo struct {
	Name          string
	Owner         string
	CommitAuthors map[CommitAuthor]struct{}
}

func processRepository(
	ctx context.Context,
	client *github.Client,
	repo *repository.Repository,
) (*RepositoryInfo, error) {
	branches, err := client.ListBranches(ctx, repo.FullName)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	// If the repository is forked, only collect commits made after the fork date.
	var since string
	if repo.Fork {
		since = repo.CreatedAt
	}

	authors := make(map[CommitAuthor]struct{})

	for _, b := range branches {
		commits, err := client.ListCommits(ctx, repo.FullName, b.Name, since)
		if err != nil {
			return nil, fmt.Errorf("list commits (branch %s): %w", b.Name, err)
		}

		for _, c := range commits {
			author := extractAuthor(c)
			if author != nil {
				authors[*author] = struct{}{}
			}
		}
	}

	owner := ""
	if repo.Owner != nil {
		owner = repo.Owner.Login
	}

	return &RepositoryInfo{
		Name:          repo.Name,
		Owner:         owner,
		CommitAuthors: authors,
	}, nil
}

func extractAuthor(c *commit.Commit) *CommitAuthor {
	if c == nil || c.Commit == nil || c.Commit.Author == nil {
		return nil
	}

	return &CommitAuthor{
		Name:         c.Commit.Author.Name,
		EmailAddress: c.Commit.Author.Email,
	}
}

// isBlockedRepository reports whether err stems from GitHub refusing to serve a repository at all, which it answers
// with 451 and does e.g. after a DMCA takedown. Such a repository is still listed among the owner's repositories, so
// nothing can be collected from it and it is not an error on part of the caller.
func isBlockedRepository(err error) bool {
	statusCodeError, ok := errors.AsType[*altshiftHttpErrors.Non2xxStatusCodeError](err)

	return ok && statusCodeError.StatusCode == http.StatusUnavailableForLegalReasons
}

func main() {
	logger := &altshiftErrorLogger.Logger{
		Logger: slog.New(
			&altshiftLog.ContextHandler{
				Next: slog.NewJSONHandler(
					os.Stderr,
					&slog.HandlerOptions{AddSource: false, Level: slog.LevelInfo},
				),
				Extractors: []altshiftLog.ContextExtractor{
					&altshiftLog.ErrorContextExtractor{
						ContextExtractors: []altshiftLog.ContextExtractor{
							&altshiftLog.ErrorContextExtractor{},
						},
					},
				},
			},
		),
	}
	slog.SetDefault(logger.Logger)

	var accessToken string
	var repoUser string
	var perRepo bool

	parser := &argument_parser.Parser{
		Description: "List the authors that appear in the commit history of a user's repositories.\n\n" +
			"The information is obtained by making requests with the GitHub REST API. " +
			"One must authenticate using a personal access token to avoid traffic limitations.\n" +
			"Scans all branches of a repository. " +
			"In case the repository is forked, only commits made after the fork date are examined.",
		Options: []option.Option{
			option.NewStringOption('t', "access-token", "A GitHub personal access token.", true, &accessToken),
			option.NewStringOption('r', "repo-user", "The username of the GitHub user whose repositories to scan.", true, &repoUser),
			option.NewBoolOption('p', "per-repo", "Show commit authors per repository.", false, &perRepo),
		},
	}

	if err := parser.Parse(); err != nil {
		if errors.Is(err, argumentParserErrors.ErrHelp) {
			os.Exit(0)
		}
		logger.FatalWithExitingMessage(
			"An error occurred when parsing arguments.",
			altshiftErrors.New(fmt.Errorf("parse: %w", err)),
		)
	}

	if accessToken == "" || repoUser == "" {
		fmt.Fprint(os.Stderr, parser.FormatHelp())
		os.Exit(1)
	}

	client := github.NewClient(
		github_config.WithFetchOptions(
			fetch_config.WithHeaders(map[string]string{
				"Authorization": "Bearer " + accessToken,
				"Accept":        "application/vnd.github.v3+json",
			}),
			fetch_config.WithRetryConfig(retry_config.New()),
		),
	)

	ctx := context.Background()

	repositories, err := client.ListRepositories(ctx, repoUser)
	if err != nil {
		logger.FatalWithExitingMessage(
			"An error occurred when listing repositories.",
			altshiftErrors.New(fmt.Errorf("list repositories: %w", err), repoUser),
		)
	}

	var mu sync.Mutex
	var repositoryInfoList []*RepositoryInfo

	var wg sync.WaitGroup
	for _, repo := range repositories {
		wg.Go(func() {
			repoInfo, err := processRepository(ctx, client, repo)
			if err != nil {
				if isBlockedRepository(err) {
					slog.Warn("Access to the repository is blocked. Skipping.", "repository", repo.FullName)
					return
				}

				logger.ErrorWithSkippingMessage(
					fmt.Sprintf("An error occurred when processing repository %s.", repo.FullName),
					altshiftErrors.New(fmt.Errorf("process repository: %w", err), repo.FullName),
				)
				return
			}

			mu.Lock()
			repositoryInfoList = append(repositoryInfoList, repoInfo)
			mu.Unlock()
		})
	}

	wg.Wait()

	if perRepo {
		var parts []string
		for _, repoInfo := range repositoryInfoList {
			authorStrings := make([]string, 0, len(repoInfo.CommitAuthors))
			for author := range repoInfo.CommitAuthors {
				authorStrings = append(authorStrings, author.String())
			}
			slices.Sort(authorStrings)

			underline := strings.Repeat("-", len(repoInfo.Name))
			parts = append(parts, repoInfo.Name+"\n"+underline+"\n"+strings.Join(authorStrings, "\n"))
		}
		fmt.Println(strings.Join(parts, "\n\n"))
	} else {
		allAuthors := make(map[CommitAuthor]struct{})
		for _, repoInfo := range repositoryInfoList {
			for author := range repoInfo.CommitAuthors {
				allAuthors[author] = struct{}{}
			}
		}

		authorStrings := make([]string, 0, len(allAuthors))
		for author := range allAuthors {
			authorStrings = append(authorStrings, author.String())
		}
		slices.Sort(authorStrings)

		fmt.Println(strings.Join(authorStrings, "\n"))
	}
}
