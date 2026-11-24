package admin

import (
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Metadata is read by logical identity only. No administrative account DTO,
// physical selector, credential or source-refresh option is accepted.
func RegisterGatewayAccountInfoRoutes(group *gin.RouterGroup, reader *service.GatewayAccountInfoReader, authorize gin.HandlerFunc) error {
	if group == nil || group.BasePath() != "/" || reader == nil || authorize == nil {
		return service.ErrGatewayNativeIdentity
	}
	read := func(legacy bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", "private, no-store")
			raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
			in, valid := DecodeGatewayAccountLimitsInput(raw)
			if err != nil || !valid {
				c.JSON(http.StatusBadRequest, gin.H{"code": "account_info_unavailable"})
				return
			}
			result := reader.Read(c.Request.Context(), in)
			if legacy {
				c.JSON(http.StatusOK, service.ProjectGatewayAccountInfoV1(result))
				return
			}
			c.JSON(http.StatusOK, result)
		}
	}
	group.POST("/private/native/v1/account-info/read", authorize, read(true))
	group.POST("/private/native/v2/account-info/read", authorize, read(false))
	return nil
}
