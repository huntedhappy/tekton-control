// File: pkg/util/ctx.go
package util

import (
        "context"
        "time"

        "github.com/google/uuid"
)

type ctxKey string

const (
        // CtxKeyReconcileID는 컨텍스트에서 Reconcile ID를 찾기 위한 키입니다.
        CtxKeyReconcileID ctxKey = "reconcileID"
)

// NewReconcileContext는 Reconcile ID가 포함된 새로운 컨텍스트와 타임아웃 함수를 생성합니다.
func NewReconcileContext(timeout time.Duration) (context.Context, context.CancelFunc, string) {
        reconcileID := uuid.New().String()
        // 부모 컨텍스트로 context.Background()를 사용합니다.
        ctx := context.WithValue(context.Background(), CtxKeyReconcileID, reconcileID)
        // 타임아웃을 포함한 새로운 컨텍스트를 반환합니다.
        ctx, cancel := context.WithTimeout(ctx, timeout)
        return ctx, cancel, reconcileID
}
