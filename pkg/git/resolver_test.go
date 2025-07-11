// File: pkg/git/resolver_test.go
package git

import (
        "testing"
)

func TestGetGitAuthFromSecret(t *testing.T) {
        // TODO: Secret 데이터로부터 인증 정보를 성공적으로 가져오는 케이스 테스트
        // TODO: username 또는 password가 없는 Secret 데이터에 대한 에러 처리 테스트
}

func TestResolver_ResolveGitSHA(t *testing.T) {
        // TODO: Git SHA를 성공적으로 가져오는 케이스 테스트
        // TODO: 캐시가 올바르게 동작하는지 테스트
        // TODO: 인증 실패 시 에러 처리 테스트
}
