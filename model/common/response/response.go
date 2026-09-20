package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
	Msg  string      `json:"msg"`
}

const (
	ERROR   = 7
	SUCCESS = 0
)

func Result(code int, data interface{}, msg string, c *gin.Context) {
	ResultWithStatus(http.StatusOK, code, data, msg, c)
}

// ResultWithStatus writes the shared response envelope with an explicit HTTP status.
// Business APIs use it when a caller needs a meaningful 4xx status as well.
func ResultWithStatus(status, code int, data interface{}, msg string, c *gin.Context) {
	c.JSON(status, Response{Code: code, Data: data, Msg: msg})
}

// Respond maps a business result to the shared response envelope. Callers pass
// their domain's forbidden sentinel without making this common package depend on
// a particular service package.
func Respond(data interface{}, err, forbiddenErr error, c *gin.Context) {
	if err != nil {
		status := http.StatusBadRequest
		if forbiddenErr != nil && errors.Is(err, forbiddenErr) {
			status = http.StatusForbidden
		}
		ResultWithStatus(status, status, nil, err.Error(), c)
		return
	}
	OkWithData(data, c)
}

func BadRequest(message string, c *gin.Context) {
	ResultWithStatus(http.StatusBadRequest, http.StatusBadRequest, nil, message, c)
}

func Unauthorized(message string, c *gin.Context) {
	ResultWithStatus(http.StatusUnauthorized, http.StatusUnauthorized, nil, message, c)
}

func Ok(c *gin.Context) {
	Result(SUCCESS, map[string]interface{}{}, "操作成功", c)
}

func OkWithMessage(message string, c *gin.Context) {
	Result(SUCCESS, map[string]interface{}{}, message, c)
}

func OkWithData(data interface{}, c *gin.Context) {
	Result(SUCCESS, data, "查询成功", c)
}

func OkWithDetailed(data interface{}, message string, c *gin.Context) {
	Result(SUCCESS, data, message, c)
}

func Fail(c *gin.Context) {
	Result(ERROR, map[string]interface{}{}, "操作失败", c)
}

func FialMissParameter(message string, c *gin.Context) {
	c.JSON(http.StatusBadRequest, Response{
		Code: 0001,
		Data: "",
		Msg:  message,
	})
}

func FailWithMessage(message string, c *gin.Context) {
	Result(ERROR, map[string]interface{}{}, message, c)
}

func FailWithDetailed(data interface{}, message string, c *gin.Context) {
	Result(ERROR, data, message, c)
}
