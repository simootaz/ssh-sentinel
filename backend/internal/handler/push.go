package handler

import (
	"context"
	"errors"
	"sync"

	"github.com/simootaz/ssh-sentinel/backend/internal/fcm"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// pushAll sends data to every registered phone, all at once, within
// cfg.PushTimeout. Delivery is best effort: an FCM failure is logged and the
// request goes on (docs/architecture.md, failure mode 14). It returns the
// number of phones the message was accepted for.
func (h *Handler) pushAll(ctx context.Context, requestID string, data map[string]string) int {
	if h.push == nil {
		return 0
	}
	devices, err := h.store.ListDevices(ctx)
	if err != nil {
		h.log.Error("push: list devices", "request", requestID, "err", err)
		return 0
	}
	if len(devices) == 0 {
		h.log.Warn("push: no device registered", "request", requestID, "type", data["type"])
		return 0
	}

	ctx, cancel := context.WithTimeout(ctx, h.cfg.PushTimeout)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for _, d := range devices {
		wg.Add(1)
		go func(d model.Device) {
			defer wg.Done()
			err := h.push.Send(ctx, d.FCMToken, data)
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
				return
			}
			if errors.Is(err, fcm.ErrUnregistered) {
				h.log.Warn("push: device token no longer registered", "request", requestID, "device", d.ID, "label", deref(d.Label))
				return
			}
			h.log.Error("push: send failed", "request", requestID, "device", d.ID, "err", err)
		}(d)
	}
	wg.Wait()
	return accepted
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
