package h

import "github.com/gin-gonic/gin"

type Lang = string

const (
	Lang_zh_CN = "zh_CN"
	Lang_en_US = "en_US"
)

func GetLanguage(c *gin.Context) Lang {
	return Lang_zh_CN
}
