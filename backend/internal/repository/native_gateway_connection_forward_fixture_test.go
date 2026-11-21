//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Each call obtains a new bounded admission, then follows the production
// binding and final native HTTP fence. Only the external provider is a fixture.
func nativeConnectionForwardFixture(t *testing.T, owner context.Context, custody *service.GatewayNativeCredentialCustody, native *service.GatewayNativeConnections, gateway *service.OpenAIGatewayService, scope service.GatewayNativeConnectionScope, route service.GatewayNativeRoute, body []byte) (*httptest.ResponseRecorder, bool, error) {
	t.Helper()
	ctx, stop := context.WithTimeout(owner, 10*time.Second)
	defer stop()
	life, err := service.NewGatewayNativeLifetime(ctx, stop, func() {}, 1<<20)
	require.NoError(t, err)
	life.BindAccount(scope.Scope.Consumer, scope.Scope.Account, scope.Scope.Generation)
	require.True(t, life.Admit(time.Now().Add(5*time.Second)))
	ctx = service.WithGatewayNativeLifetime(service.WithGatewayNativeCustody(ctx, custody), life)
	bound, handled, err := native.BindRoute(ctx, route, scope.Scope.Account, 1, 1<<20, 512)
	recorder := httptest.NewRecorder()
	if err != nil {
		life.NoForward()
		return recorder, false, err
	}
	require.True(t, handled)
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Authorization", "Bearer foreign-consumer-token")
	c.Request.Header.Set("Cookie", "foreign-browser-session")
	c.Request.Header.Set("X-Codex-Turn-State", "foreign-affinity")
	_, entered, err := gateway.ForwardGatewayRoute(bound, c, route, body)
	require.True(t, life.Snapshot().ForwardingReturned)
	return recorder, entered, err
}
