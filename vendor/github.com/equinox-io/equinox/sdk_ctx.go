// +build go1.7

package equinox

import (
	"context"
)

// CheckContext is like Check but includes a context.
func CheckContext(ctx context.Context, appID string, opts Options) (Response, error) {
	var req, err = checkRequest(appID, &opts)

	if err != nil {
		return Response{}, err
	}

	return doCheckRequest(opts, req.WithContext(ctx))
}

// ApplyContext is like Apply but includes a context.
func (r Response) ApplyContext(ctx context.Context) error {
	var req, opts, err = r.applyRequest()

	if err != nil {
		return err
	}

	return r.applyUpdate(req.WithContext(ctx), opts)
}
