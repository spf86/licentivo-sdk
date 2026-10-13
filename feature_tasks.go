package licentivo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A task journal stores identifiers and outcome, never business data or keys.
type featureTask struct {
	Version       int    `json:"version"`
	OperationID   string `json:"operation_id"`
	Feature       string `json:"feature"`
	Quantity      int64  `json:"quantity"`
	ReservationID string `json:"reservation_id"`
	State         string `json:"state"`
}

func (c *Client) taskPath(id string) (string, error) {
	if id == "" || len(id) > 80 || strings.IndexFunc(id, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return "", errors.New("a stable operationID of 1-80 non-whitespace ASCII characters is required")
	}
	return c.cfg.CachePath + ".task-" + licenseHash(c.cacheLicenseHash()+"\n"+id) + ".json", nil
}
func readFeatureTask(path, id string) (featureTask, error) {
	var task featureTask
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return featureTask{Version: 1, OperationID: id}, nil
	}
	if err != nil {
		return task, err
	}
	if len(b) > 4096 || json.Unmarshal(b, &task) != nil || task.Version != 1 || task.OperationID != id || task.Feature == "" || task.Quantity <= 0 || task.ReservationID == "" {
		return task, errors.New("invalid feature task journal; do not repeat the business action")
	}
	return task, nil
}
func saveFeatureTask(path string, task featureTask) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(task)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".feature-task-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// RunMeteredFeature reserves, runs the business action, and commits/cancels.
// Reuse operationID for a retry. A recorded pending commit NEVER reruns action.
// Only one workflow may run per client; separate processes must use different job IDs.
func (c *Client) RunMeteredFeature(ctx context.Context, feature string, quantity int64, operationID string, action func() error) error {
	if action == nil || quantity <= 0 {
		return errors.New("business action and positive quantity are required")
	}
	if !c.featureTaskMu.TryLock() {
		return errors.New("another feature task is running")
	}
	defer c.featureTaskMu.Unlock()
	path, err := c.taskPath(operationID)
	if err != nil {
		return err
	}
	task, err := readFeatureTask(path, operationID)
	if err != nil {
		return err
	}
	if task.State != "" && (task.Feature != feature || task.Quantity != quantity) {
		return errors.New("operation ID was already used for another feature or quantity")
	}
	task.Feature, task.Quantity = feature, quantity
	switch task.State {
	case "committed":
		return nil // No second business execution.
	case "commit_pending":
		return c.finishFeatureTask(ctx, path, task, true)
	case "cancel_pending":
		if err = c.finishFeatureTask(ctx, path, task, false); err != nil {
			return err
		}
		return errors.New("operation was canceled; use a new job ID for a new action")
	case "canceled":
		return errors.New("operation was canceled; use a new job ID for a new action")
	case "business_running":
		return errors.New("business outcome unknown; verify its result and call ResolveMeteredFeature, never repeat it blindly")
	case "":
	default:
		return errors.New("invalid feature task state")
	}
	if err = c.RunFeature(feature, func() error { return nil }); err != nil {
		return err
	}
	hold, err := c.Reserve(ctx, feature, quantity, operationID)
	if err != nil {
		return err
	}
	if hold.Status == "committed" {
		task.ReservationID, task.State = hold.ID, "committed"
		return saveFeatureTask(path, task)
	}
	if hold.Status != "pending" {
		return errors.New("reservation is closed; the business action was not run")
	}
	task.ReservationID, task.State = hold.ID, "business_running"
	if err = saveFeatureTask(path, task); err != nil {
		_, _ = c.Cancel(ctx, hold.ID, operationID)
		return err
	}
	if businessErr := action(); businessErr != nil {
		task.State = "cancel_pending"
		if err = saveFeatureTask(path, task); err == nil {
			err = c.finishFeatureTask(ctx, path, task, false)
		}
		return errors.Join(businessErr, err)
	}
	task.State = "commit_pending"
	if err = saveFeatureTask(path, task); err != nil {
		return fmt.Errorf("business completed; task journal update failed; do not repeat operation %s: %w", operationID, err)
	}
	return c.finishFeatureTask(ctx, path, task, true)
}

func (c *Client) finishFeatureTask(ctx context.Context, path string, task featureTask, succeeded bool) error {
	var hold Reservation
	var err error
	if succeeded {
		hold, err = c.Commit(ctx, task.ReservationID, task.OperationID)
	} else {
		hold, err = c.Cancel(ctx, task.ReservationID, task.OperationID)
	}
	if err != nil {
		return fmt.Errorf("confirmation pending for operation %s; do not repeat business: %w", task.OperationID, err)
	}
	if succeeded && hold.Status != "committed" || !succeeded && hold.Status != "canceled" {
		return errors.New("reservation closed without the requested outcome; do not repeat business")
	}
	task.State = hold.Status
	return saveFeatureTask(path, task)
}

// ResolveMeteredFeature settles a recorded task only; it never executes business.
// For business_running after a crash, succeeded must come from your durable result.
func (c *Client) ResolveMeteredFeature(ctx context.Context, operationID string, succeeded bool) error {
	if !c.featureTaskMu.TryLock() {
		return errors.New("another feature task is running")
	}
	defer c.featureTaskMu.Unlock()
	path, err := c.taskPath(operationID)
	if err != nil {
		return err
	}
	task, err := readFeatureTask(path, operationID)
	if err != nil {
		return err
	}
	if task.State == "committed" && succeeded || task.State == "canceled" && !succeeded {
		return nil
	}
	if task.ReservationID == "" || task.State != "business_running" && task.State != "commit_pending" && task.State != "cancel_pending" || task.State == "commit_pending" && !succeeded || task.State == "cancel_pending" && succeeded {
		return errors.New("task cannot be resolved with that outcome")
	}
	if succeeded {
		task.State = "commit_pending"
	} else {
		task.State = "cancel_pending"
	}
	if err = saveFeatureTask(path, task); err != nil {
		return err
	}
	return c.finishFeatureTask(ctx, path, task, succeeded)
}
