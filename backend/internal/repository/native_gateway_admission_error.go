package repository

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Call only at protected dispatch storage admission, before provider entry.
// Driver codes are finite evidence; error text and generic failures are not.
func gatewayNativeAdmissionError(err error) error {
	var failure *pq.Error
	if errors.As(err, &failure) && (failure.Code == "40001" || failure.Code == "40P01") {
		return service.ErrGatewayNativeAdmissionStorageConflict
	}
	return service.ErrGatewayNativeIdentity
}
