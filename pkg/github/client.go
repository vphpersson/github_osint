package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	"github.com/altshiftab/utils_go/pkg/errors/types/empty_error"
	motmedelHttpErrors "github.com/altshiftab/utils_go/pkg/http/errors"
	"github.com/altshiftab/utils_go/pkg/http/types/fetch_config"
	motmedelHttpUtils "github.com/altshiftab/utils_go/pkg/http/utils"

	"github.com/Motmedel/github_osint/pkg/github/github_config"
	"github.com/Motmedel/github_osint/pkg/github/types/branch"
	"github.com/Motmedel/github_osint/pkg/github/types/commit"
	"github.com/Motmedel/github_osint/pkg/github/types/repository"
)

const perPageParameter = "per_page"

const MaxNumResultsPerPage = 100

var defaultBaseUrl = &url.URL{
	Scheme: "https",
	Host:   "api.github.com",
}

type Client struct {
	baseUrl *url.URL
	config  *github_config.Config
}

func NewClient(options ...github_config.Option) *Client {
	return NewClientWithBaseUrl(defaultBaseUrl, options...)
}

func NewClientWithBaseUrl(baseUrl *url.URL, options ...github_config.Option) *Client {
	u := *baseUrl
	u.Path = "/"

	return &Client{baseUrl: &u, config: github_config.New(options...)}
}

// ListRepositories fetches all repositories for a given username, handling pagination.
func (c *Client) ListRepositories(ctx context.Context, username string, options ...fetch_config.Option) ([]*repository.Repository, error) {
	if username == "" {
		return nil, altshiftErrors.NewWithTrace(empty_error.New("username"))
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context err: %w", err)
	}

	var allRepositories []*repository.Repository

	for pageNumber := 1; ; pageNumber++ {
		u := *c.baseUrl
		u.Path += "users/" + url.PathEscape(username) + "/repos"
		u.RawQuery = url.Values{
			perPageParameter: {strconv.Itoa(MaxNumResultsPerPage)},
			"page":           {strconv.Itoa(pageNumber)},
		}.Encode()
		urlString := u.String()

		fetchOptions := append(c.config.FetchOptions, options...)
		_, repositories, err := motmedelHttpUtils.FetchJson[[]*repository.Repository](ctx, urlString, fetchOptions...)
		if err != nil {
			return nil, altshiftErrors.New(fmt.Errorf("fetch json: %w", err), urlString)
		}

		allRepositories = append(allRepositories, repositories...)

		if len(repositories) < MaxNumResultsPerPage {
			break
		}
	}

	return allRepositories, nil
}

// ListBranches fetches all branch names for a given repository, handling pagination.
func (c *Client) ListBranches(ctx context.Context, fullName string, options ...fetch_config.Option) ([]*branch.Branch, error) {
	if fullName == "" {
		return nil, altshiftErrors.NewWithTrace(empty_error.New("full name"))
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context err: %w", err)
	}

	var allBranches []*branch.Branch

	for pageNumber := 1; ; pageNumber++ {
		u := *c.baseUrl
		u.Path += "repos/" + fullName + "/branches"
		u.RawQuery = url.Values{
			perPageParameter: {strconv.Itoa(MaxNumResultsPerPage)},
			"page":           {strconv.Itoa(pageNumber)},
		}.Encode()
		urlString := u.String()

		fetchOptions := append(c.config.FetchOptions, options...)
		_, branches, err := motmedelHttpUtils.FetchJson[[]*branch.Branch](ctx, urlString, fetchOptions...)
		if err != nil {
			return nil, altshiftErrors.New(fmt.Errorf("fetch json: %w", err), urlString)
		}

		allBranches = append(allBranches, branches...)

		if len(branches) < MaxNumResultsPerPage {
			break
		}
	}

	return allBranches, nil
}

// ListCommits fetches commits for a given repository and branch, handling pagination.
// If since is non-empty, only commits after that date are returned (ISO 8601 format).
// Returns nil, nil if the repository has no commits (HTTP 409).
func (c *Client) ListCommits(ctx context.Context, fullName string, branchName string, since string, options ...fetch_config.Option) ([]*commit.Commit, error) {
	if fullName == "" {
		return nil, altshiftErrors.NewWithTrace(empty_error.New("full name"))
	}
	if branchName == "" {
		return nil, altshiftErrors.NewWithTrace(empty_error.New("branch name"))
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context err: %w", err)
	}

	var allCommits []*commit.Commit

	for pageNumber := 1; ; pageNumber++ {
		u := *c.baseUrl
		u.Path += "repos/" + fullName + "/commits"

		query := url.Values{
			perPageParameter: {strconv.Itoa(MaxNumResultsPerPage)},
			"page":           {strconv.Itoa(pageNumber)},
			"sha":            {branchName},
		}
		if since != "" {
			query.Set("since", since)
		}

		u.RawQuery = query.Encode()
		urlString := u.String()

		fetchOptions := append(c.config.FetchOptions, options...)
		fetchOptions = append(fetchOptions, fetch_config.WithSkipErrorOnStatus(true))

		response, commits, err := motmedelHttpUtils.FetchJson[[]*commit.Commit](ctx, urlString, fetchOptions...)
		if err != nil {
			return nil, altshiftErrors.New(fmt.Errorf("fetch json: %w", err), urlString)
		}

		// HTTP 409 means the repository has no commits.
		if response != nil && response.StatusCode == http.StatusConflict {
			return nil, nil
		}

		if response != nil && response.StatusCode/100 != 2 {
			return nil, altshiftErrors.New(
				&motmedelHttpErrors.Non2xxStatusCodeError{StatusCode: response.StatusCode},
				urlString,
			)
		}

		allCommits = append(allCommits, commits...)

		if len(commits) < MaxNumResultsPerPage {
			break
		}
	}

	return allCommits, nil
}

// ListRepositories fetches all repositories, ignoring a non-2xx status code, which may occur if the user does not
// exist.
func (c *Client) ListRepositoriesIgnoreError(ctx context.Context, username string, options ...fetch_config.Option) ([]*repository.Repository, error) {
	repositories, err := c.ListRepositories(ctx, username, options...)
	if err != nil {
		if errors.Is(err, motmedelHttpErrors.ErrNon2xxStatusCode) {
			return nil, nil
		}
		return nil, err
	}

	return repositories, nil
}
