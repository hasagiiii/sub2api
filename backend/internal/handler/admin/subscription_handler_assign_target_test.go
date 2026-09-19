//go:build unit

package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 分配订阅只能二选一：分组或套餐。
//
// 同时给出两者时没有合理的取舍——分组与套餐各自带一套分组集合与有效期，静默偏向
// 其中一个会让另一个看起来也生效了；两者都不给则根本没有分配目标。
func TestValidateAssignTarget(t *testing.T) {
	planID := int64(7)

	t.Run("group only", func(t *testing.T) {
		require.NoError(t, validateAssignTarget(3, nil))
	})

	t.Run("plan only", func(t *testing.T) {
		require.NoError(t, validateAssignTarget(0, &planID))
	})

	t.Run("neither", func(t *testing.T) {
		require.Error(t, validateAssignTarget(0, nil))
	})

	t.Run("both", func(t *testing.T) {
		require.Error(t, validateAssignTarget(3, &planID))
	})

	// plan_id 显式传 0 等同于未指定，不能被当成"指定了套餐"而放过没有分组的请求。
	t.Run("zero plan id counts as unspecified", func(t *testing.T) {
		zero := int64(0)
		require.Error(t, validateAssignTarget(0, &zero))
		require.NoError(t, validateAssignTarget(3, &zero))
	})
}
