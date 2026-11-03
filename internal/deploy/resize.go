package deploy

import (
	"errors"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// ResizeReconnectHint turns a resize refused because the cloud account must
// be reconnected into an error that links the dashboard's resize page for
// the cluster.
func ResizeReconnectHint(dashboard, org, clusterID string, err error) error {
	var apiErr *portal.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "reconnect_required" {
		return err
	}
	return &reconnectHint{
		msg: fmt.Sprintf("%s. Reconnect it from the dashboard: %s/orgs/%s/clusters/%s/resize",
			strings.TrimRight(apiErr.Message, "."), dashboard, org, clusterID),
		api: apiErr,
	}
}

// reconnectHint reads as the hint and keeps the API's answer in the chain,
// so its code still reaches the caller.
type reconnectHint struct {
	msg string
	api *portal.Error
}

func (e *reconnectHint) Error() string { return e.msg }
func (e *reconnectHint) Unwrap() error { return e.api }
