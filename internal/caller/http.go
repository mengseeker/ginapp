/*
外部服务调用
传递request-id,traceid等
*/
package caller

import (
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

type HttpClient = resty.Client

var (
	DeliverHeaders = []string{
		"X-Request-Id",
	}
)

func NewClient(c *gin.Context) *HttpClient {
	hc := resty.New()
	for _, h := range DeliverHeaders {
		if id, exist := c.Get(h); exist {
			hc.SetHeader(h, id.(string))
		}
	}

	return hc
}
