//go:build linux

package gatewaybootstrap

import (
	"context"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// The existing protected management lane owns authentication, deadline and
// concurrency. These routes reuse strict logical selectors and native owners;
// neither numeric account IDs nor submitted model snapshots are accepted.
func registerPrivateConnectionAccountRoutes(router *gin.Engine, oauth *privateOAuth, authorize gin.HandlerFunc) error {
	if router == nil || oauth == nil || authorize == nil {
		return ErrDenied
	}
	authority, ok := oauth.owners.(NativeAccountLimitsAuthority)
	if !ok {
		return ErrDenied
	}
	authorizeRead := func(ctx context.Context, in service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
		consumer, err := service.GatewayNativeConsumer(ctx)
		if err != nil {
			return service.GatewayNativeRoute{}, ErrDenied
		}
		return authority.AuthorizeNativeAccountLimits(ctx, consumer, in)
	}
	readBody := func(c *gin.Context) ([]byte, bool) {
		c.Header("Cache-Control", "private, no-store")
		request := c.Request
		if request.URL.RawPath != "" || request.URL.RawQuery != "" || request.URL.ForceQuery ||
			request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Content-Encoding") != "" || request.ContentLength > 4096 {
			return nil, false
		}
		raw, err := io.ReadAll(io.LimitReader(request.Body, 4097))
		return raw, err == nil && len(raw) <= 4096
	}
	if oauth.nativeConnections != nil || oauth.models != nil || oauth.legacyModels != nil {
		models, err := service.NewGatewayAccountModelsReaderV2(oauth.nativeConnections, oauth.models, authorizeRead, oauth.legacyModels)
		if err != nil {
			return err
		}
		router.POST("/private/native/v2/account-models/read", authorize, func(c *gin.Context) {
			raw, bounded := readBody(c)
			in, valid := admin.DecodeGatewayAccountModelsInput(raw)
			if !bounded || !valid {
				c.JSON(http.StatusBadRequest, gin.H{"code": "account_models_unavailable"})
				return
			}
			c.JSON(http.StatusOK, models.Read(c.Request.Context(), in))
		})
	}
	if oauth.nativeConnections != nil || oauth.limits != nil || oauth.limitsV2 != nil {
		limits, err := service.NewGatewayAccountLimitsReaderV3(oauth.nativeConnections, oauth.limits, oauth.limitsV2, authorizeRead)
		if err != nil {
			return err
		}
		router.GET("/private/native/v3/account-limits/capabilities", authorize, func(c *gin.Context) {
			c.Header("Cache-Control", "private, no-store")
			c.JSON(http.StatusOK, service.GatewayAccountLimitsCapabilities{Contract: service.GatewayAccountLimitsContractV3, CachedRead: true, QuotaOnlyRefresh: false})
		})
		router.POST("/private/native/v3/account-limits/read", authorize, func(c *gin.Context) {
			raw, bounded := readBody(c)
			in, valid := admin.DecodeGatewayAccountLimitsInput(raw)
			if !bounded || !valid {
				c.JSON(http.StatusBadRequest, gin.H{"code": "account_limits_unavailable"})
				return
			}
			c.JSON(http.StatusOK, limits.Read(c.Request.Context(), in))
		})
	}
	return nil
}
