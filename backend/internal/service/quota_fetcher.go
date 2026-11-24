package service

import (
	"context"
)

// QuotaFetcher 额度获取接口，各平台实现此接口
type QuotaFetcher interface {
	// CanFetch 检查是否可以获取此账户的额度
	CanFetch(account *Account) bool
	// FetchQuota 获取账户额度信息
	FetchQuota(ctx context.Context, account *Account, proxyURL string) (*QuotaResult, error)
}

// QuotaResult 额度获取结果
type QuotaResult struct {
	UsageInfo *UsageInfo     // 转换后的使用信息
	Raw       map[string]any // 原始响应，可存入 account.Extra
	// Transient output of the current authenticated collector, never reconstructed
	// from Raw or cached UsageInfo. The caller supplies the pre-HTTP identity/order.
	AccountInfo *GatewayAccountInfoFields
}
