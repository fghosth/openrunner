package core

import "fmt"

// ValidationError 表示领域对象违反了不变量。
type ValidationError struct{ Field, Reason string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// Athlete 是一个运动员的完整数据域：活动、区间配置、赛季、度量结果都挂在 Athlete 下。
// 单用户安装可拥有多个 Athlete 档案（见 CONTEXT.md）。
type Athlete struct {
	ID   int64
	Name string
}

// Validate 检查 Athlete 的领域不变量。
func (a Athlete) Validate() error {
	if a.Name == "" {
		return &ValidationError{Field: "Name", Reason: "不能为空"}
	}
	return nil
}
