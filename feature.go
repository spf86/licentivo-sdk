package licentivo

import "fmt"

// RunFeature checks the local signed entitlement before invoking the business
// action. It does not consume a metered allowance; use RunMeteredFeature for that.
func (c *Client) RunFeature(feature string, action func() error) error {
	if action == nil {
		return fmt.Errorf("business action is required")
	}
	status := c.FeatureStatus(feature)
	if !status.Allowed {
		return fmt.Errorf("feature denied: %s (%s); request_id=%s", status.Code, status.State, status.RequestID)
	}
	return action()
}
