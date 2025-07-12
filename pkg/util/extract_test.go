// File: pkg/util/extract_test.go
package util

import (
	"testing"
)

func TestExtractProjectName(t *testing.T) {
	testCases := []struct {
		name     string
		repoURL  string
		expected string
	}{
		{"Standard HTTPS", "https://gitlab.com/user/my-project.git", "my-project"},
		{"HTTPS without .git", "https://github.com/tektoncd/pipeline", "pipeline"},
		{"SSH URL", "git@github.com:tektoncd/triggers.git", "triggers"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := ExtractProjectName(tc.repoURL); actual != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, actual)
			}
		})
	}
}
