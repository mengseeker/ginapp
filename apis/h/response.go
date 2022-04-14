package h

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
	RequestID string      `json:"requestID,omitempty"`
}

func R(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:      code,
		Message:   message,
		Data:      data,
		CreatedAt: time.Now(),
		RequestID: GetRequestID(c),
	})
}

func RR(c *gin.Context, data interface{}) {
	R(c, CodeOK, "success", data)
}

func RE(c *gin.Context, msg string) {
	R(c, CodeExceptionDefault, msg, nil)
}
