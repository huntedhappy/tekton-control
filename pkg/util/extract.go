// File: pkg/util/extract.go
package util

import (
	"strings"
)

var PvcWorkspaceKeywords = []string{"data", "workspace", "output", "source"}

// ExtractProjectName은 Git URL에서 프로젝트 이름을 추출합니다.
func ExtractProjectName(repoURL string) string {
	cleanedURL := strings.TrimSuffix(repoURL, ".git")
	cleanedURL = strings.TrimPrefix(cleanedURL, "https://")
	cleanedURL = strings.TrimPrefix(cleanedURL, "ssh://")
	if strings.HasPrefix(cleanedURL, "git@") {
		parts := strings.Split(cleanedURL, ":")
		if len(parts) > 1 {
			cleanedURL = parts[1]
		}
	}
	parts := strings.Split(cleanedURL, "/")
	return parts[len(parts)-1]
}

// IsPvcWorkspace는 워크스페이스 이름에 따라 PVC 사용 여부를 결정합니다.
func IsPvcWorkspace(wsName string) bool {
	lower := strings.ToLower(wsName)
	for _, kw := range PvcWorkspaceKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
