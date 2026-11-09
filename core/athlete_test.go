package core

import (
	"errors"
	"testing"
)

func TestAthleteValidate(t *testing.T) {
	t.Run("有名字的 Athlete 合法", func(t *testing.T) {
		a := Athlete{Name: "我-公路"}
		if err := a.Validate(); err != nil {
			t.Fatalf("期望合法, 得到 %v", err)
		}
	})

	t.Run("空名字的 Athlete 不合法", func(t *testing.T) {
		a := Athlete{Name: ""}
		err := a.Validate()
		if err == nil {
			t.Fatal("期望校验失败")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != "Name" {
			t.Fatalf("期望 Name 校验错误, 得到 %v", err)
		}
	})
}
