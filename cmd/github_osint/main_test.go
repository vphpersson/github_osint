package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	altshiftHttpErrors "github.com/altshiftab/utils_go/pkg/http/errors"
)

// wrapLikeProcessRepository wraps err the way the call chain from main down to the fetch does.
func wrapLikeProcessRepository(err error) error {
	return altshiftErrors.New(
		fmt.Errorf(
			"process repository: %w",
			fmt.Errorf("list branches: %w", altshiftErrors.New(fmt.Errorf("fetch json: %w", err))),
		),
	)
}

func TestIsBlockedRepository(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil"},
		{name: "unrelated error", err: errors.New("test")},
		{
			name:     "blocked",
			err:      wrapLikeProcessRepository(&altshiftHttpErrors.Non2xxStatusCodeError{StatusCode: http.StatusUnavailableForLegalReasons}),
			expected: true,
		},
		{
			name:     "bare blocked",
			err:      &altshiftHttpErrors.Non2xxStatusCodeError{StatusCode: http.StatusUnavailableForLegalReasons},
			expected: true,
		},
		{
			name: "other status code",
			err:  wrapLikeProcessRepository(&altshiftHttpErrors.Non2xxStatusCodeError{StatusCode: http.StatusNotFound}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isBlockedRepository(testCase.err); got != testCase.expected {
				t.Errorf("isBlockedRepository() = %v, expected %v", got, testCase.expected)
			}
		})
	}
}
