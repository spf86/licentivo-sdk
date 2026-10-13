package licentivo

import (
	"context"
	"errors"
	"time"
)

type Reservation struct {
	ID          string      `json:"reservation_id"`
	OperationID string      `json:"operation_id"`
	Status      string      `json:"status"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Consumption Consumption `json:"consumption"`
}

// Reserve retains operationID with the business job. A committed result means
// the business operation has already finished: never run it again on replay.
func (c *Client) Reserve(ctx context.Context, feature string, quantity int64, operationID string) (Reservation, error) {
	return c.reservation(ctx, "reserve", operationID, map[string]any{"feature": feature, "quantity": quantity})
}
func (c *Client) Commit(ctx context.Context, reservationID, operationID string) (Reservation, error) {
	return c.reservation(ctx, "commit", operationID, map[string]any{"reservation_id": reservationID})
}
func (c *Client) Cancel(ctx context.Context, reservationID, operationID string) (Reservation, error) {
	return c.reservation(ctx, "cancel", operationID, map[string]any{"reservation_id": reservationID})
}
func (c *Client) reservation(ctx context.Context, op, id string, extra map[string]any) (Reservation, error) {
	if id == "" {
		return Reservation{}, errors.New("retain operationID for retries")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	claims, err := c.check()
	if err != nil {
		return Reservation{}, err
	}
	out, _, err := c.requestExtra(ctx, "consume/"+op, claims.ActivationID, id, extra)
	if err != nil {
		if deniedAuthorization(err) {
			c.invalidate()
		}
		c.publish(false, err)
		return Reservation{}, err
	}
	if out.Reservation == nil {
		return Reservation{}, errors.New("server omitted reservation receipt")
	}
	c.publish(true, nil)
	return *out.Reservation, nil
}
