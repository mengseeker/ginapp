package h

import (
	"bytes"
	_ "embed"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
)

const (
	CodeServerError      = -1
	CodeOK               = 0
	CodeExceptionDefault = 110
)

type Exception struct {
	Code      int             `yaml:"code"`
	Languages map[Lang]string `yaml:"languages"`
}

var (
	//go:embed exceptions.yaml
	exceptions        string
	definedExceptions = func() map[string]*Exception {
		var e map[string]*Exception
		if err := yaml.NewDecoder(bytes.NewBufferString(exceptions)).Decode(&e); err != nil {
			panic(err)
		}
		for _, v := range e {
			if v.Code == 0 {
				v.Code = CodeExceptionDefault
			}
		}
		return e
	}()
)

func ParseException(lang Lang, key string, vals ...interface{}) (int, string) {
	if e, ok := definedExceptions[key]; ok {
		if tpl := e.Languages[lang]; tpl != "" {
			return e.Code, fmt.Sprintf(tpl, vals...)
		}
	}
	return CodeExceptionDefault, key
}

// 通过panic方式直接返回异常
// 由于多线程下，panic不安全，强制要求只能再apis下使用
func AbortDirect(c *gin.Context, key string, vals ...interface{}) {
	code, msg := ParseException(GetLanguage(c), key, vals...)
	panic(Response{
		Code:      code,
		Message:   msg,
		CreatedAt: time.Now(),
		RequestID: GetRequestID(c),
	})
}
