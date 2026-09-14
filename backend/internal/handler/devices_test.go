package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// devicesContractKeys are the keys of a device in the contract, and nothing
// else: the FCM token must never be returned.
var devicesContractKeys = []string{"id", "platform", "label", "created_at", "last_seen_at"}

// devicesMustHaveExactKeys fails unless m has exactly the given keys. Shared
// by the devices, blocked-ips and geo-rules tests.
func devicesMustHaveExactKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %q in %v", k, m)
		}
	}
	if len(m) != len(keys) {
		t.Fatalf("got %d keys in %v, want exactly %v", len(m), m, keys)
	}
}

const devicesToken = "dEf4-fcm-token-of-the-pixel"

func devicesBody(token, platform string, label *string) model.DeviceBody {
	return model.DeviceBody{FCMToken: token, Platform: platform, Label: label}
}

func devicesList(e *env) []model.DeviceItem {
	e.t.Helper()
	st, body := e.admin(http.MethodGet, "/devices", nil)
	mustStatus(e.t, st, http.StatusOK, body)
	return decode[model.ListResponse[model.DeviceItem]](e.t, body).Items
}

func TestDevicesCreate(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", devicesBody(devicesToken, model.PlatformAndroid, strp("Pixel 8")))
	mustStatus(t, st, http.StatusCreated, body)

	devicesMustHaveExactKeys(t, decodeMap(t, body), devicesContractKeys...)
	if strings.Contains(string(body), "fcm") || strings.Contains(string(body), devicesToken) {
		t.Fatalf("fcm token leaked: %s", body)
	}
	item := decode[model.DeviceItem](t, body)
	if !isUUID(item.ID) {
		t.Fatalf("id %q is not a UUID", item.ID)
	}
	if item.Platform != model.PlatformAndroid || item.Label == nil || *item.Label != "Pixel 8" {
		t.Fatalf("body %s", body)
	}
	if !item.CreatedAt.Equal(baseTime) || item.LastSeenAt == nil || !item.LastSeenAt.Equal(baseTime) {
		t.Fatalf("times: %s", body)
	}
}

func TestDevicesCreateAgainRefreshes(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", devicesBody(devicesToken, model.PlatformAndroid, strp("Pixel 8")))
	mustStatus(t, st, http.StatusCreated, body)
	first := decode[model.DeviceItem](t, body)

	// Same token later, with a new label and platform: refreshed, not duplicated.
	e.clock.Advance(time.Hour)
	st, body = e.admin(http.MethodPost, "/devices", devicesBody(devicesToken, model.PlatformIOS, strp("Pixel 8 (work)")))
	mustStatus(t, st, http.StatusOK, body)
	again := decode[model.DeviceItem](t, body)
	if again.ID != first.ID {
		t.Fatalf("id changed: %q then %q", first.ID, again.ID)
	}
	if again.Label == nil || *again.Label != "Pixel 8 (work)" || again.Platform != model.PlatformIOS {
		t.Fatalf("body %s", body)
	}
	if !again.CreatedAt.Equal(baseTime) || again.LastSeenAt == nil || !again.LastSeenAt.Equal(baseTime.Add(time.Hour)) {
		t.Fatalf("times: %s", body)
	}
	if n := len(devicesList(e)); n != 1 {
		t.Fatalf("%d devices, want 1", n)
	}
}

func TestDevicesCreateLabelAbsentIsNull(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", []byte(`{"fcm_token": "tok-1", "platform": "android"}`))
	mustStatus(t, st, http.StatusCreated, body)
	if v, ok := decodeMap(t, body)["label"]; !ok || v != nil {
		t.Fatalf("label: want present and null, body %s", body)
	}

	// A blank label is the same as no label.
	st, body = e.admin(http.MethodPost, "/devices", []byte(`{"fcm_token": "tok-2", "platform": "ios", "label": "   "}`))
	mustStatus(t, st, http.StatusCreated, body)
	if v := decodeMap(t, body)["label"]; v != nil {
		t.Fatalf("blank label: want null, body %s", body)
	}
}

func TestDevicesCreateEmptyTokenIs400(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "   "} {
		st, body := e.admin(http.MethodPost, "/devices", devicesBody(tok, model.PlatformAndroid, nil))
		mustStatus(t, st, http.StatusBadRequest, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("body %s", body)
		}
	}
}

func TestDevicesCreateBadPlatformIs400(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"", "windows", "Android"} {
		st, body := e.admin(http.MethodPost, "/devices", devicesBody(devicesToken, p, nil))
		mustStatus(t, st, http.StatusBadRequest, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("body %s", body)
		}
	}
	if n := len(devicesList(e)); n != 0 {
		t.Fatalf("%d devices registered by rejected calls, want 0", n)
	}
}

func TestDevicesCreateInvalidJSONIs400(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", []byte(`{"fcm_token": `))
	mustStatus(t, st, http.StatusBadRequest, body)
	if decode[model.ErrorResponse](t, body).Error == "" {
		t.Fatalf("body %s", body)
	}
}

func TestDevicesListInCreationOrder(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", devicesBody("tok-pixel", model.PlatformAndroid, strp("Pixel 8")))
	mustStatus(t, st, http.StatusCreated, body)
	e.clock.Advance(time.Minute)
	st, body = e.admin(http.MethodPost, "/devices", devicesBody("tok-iphone", model.PlatformIOS, strp("iPhone 15")))
	mustStatus(t, st, http.StatusCreated, body)

	st, body = e.admin(http.MethodGet, "/devices", nil)
	mustStatus(t, st, http.StatusOK, body)
	if strings.Contains(string(body), "tok-") || strings.Contains(string(body), "fcm") {
		t.Fatalf("fcm token leaked: %s", body)
	}
	items := decode[model.ListResponse[model.DeviceItem]](t, body).Items
	if len(items) != 2 || *items[0].Label != "Pixel 8" || *items[1].Label != "iPhone 15" {
		t.Fatalf("want Pixel 8 then iPhone 15: %s", body)
	}
	raw := decodeMap(t, body)["items"].([]any)
	for _, it := range raw {
		devicesMustHaveExactKeys(t, it.(map[string]any), devicesContractKeys...)
	}
}

func TestDevicesListEmptyIsNotNull(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/devices", nil)
	mustStatus(t, st, http.StatusOK, body)
	items, ok := decodeMap(t, body)["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("want items: [], got %s", body)
	}
}

func TestDevicesDelete(t *testing.T) {
	e := newEnv(t)
	keep := e.addDevice("Pixel 8")
	gone := e.addDevice("Old phone")

	st, body := e.admin(http.MethodDelete, "/devices/"+gone.ID, nil)
	mustStatus(t, st, http.StatusNoContent, body)
	if len(body) != 0 {
		t.Fatalf("204 with a body: %s", body)
	}

	st, body = e.admin(http.MethodDelete, "/devices/"+gone.ID, nil)
	mustStatus(t, st, http.StatusNotFound, body)

	items := devicesList(e)
	if len(items) != 1 || items[0].ID != keep.ID {
		t.Fatalf("after delete: %+v", items)
	}
}

func TestDevicesDeleteUnknownIs404(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{dbtest.NewID(), "not-a-uuid", "1234"} {
		st, body := e.admin(http.MethodDelete, "/devices/"+id, nil)
		mustStatus(t, st, http.StatusNotFound, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("body %s", body)
		}
	}
}

func TestDevicesServerTokenIs403(t *testing.T) {
	e := newEnv(t)
	d := e.addDevice("Pixel 8")
	calls := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/devices", devicesBody(devicesToken, model.PlatformAndroid, nil)},
		{http.MethodGet, "/devices", nil},
		{http.MethodDelete, "/devices/" + d.ID, nil},
	}
	for _, c := range calls {
		st, body := e.agent(c.method, c.path, c.body)
		mustStatus(t, st, http.StatusForbidden, body)
	}
	if n := len(devicesList(e)); n != 1 {
		t.Fatalf("devices changed by a forbidden call: %d", n)
	}
}

// DELETE /devices: "it stops receiving pushes at once. History rows keep the
// label." The row is kept with deleted_at set, so the join still finds the
// label; registering the same FCM token again brings the phone back.
func TestDevicesDeleteKeepsHistoryLabel(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/devices", devicesBody("fcm-pixel-8", model.PlatformAndroid, strp("Pixel 8")))
	mustStatus(t, st, http.StatusCreated, body)
	dev := decode[model.DeviceItem](t, body)

	req, err := e.store.CreateRequest(context.Background(), &model.Request{
		ServerID: e.server.ID, Context: model.ContextSSH, Username: "deploy", Hostname: "web-01",
		SourceIP: strp("203.0.113.42"), Status: model.StatusPending,
		CreatedAt: e.clock.Now(), ExpiresAt: e.clock.Now().Add(30 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	st, body = e.admin(http.MethodPost, "/verdict", model.VerdictBody{RequestID: req.ID, Verdict: model.VerdictApprove, DeviceID: &dev.ID})
	mustStatus(t, st, http.StatusOK, body)

	st, body = e.admin(http.MethodDelete, "/devices/"+dev.ID, nil)
	mustStatus(t, st, http.StatusNoContent, body)

	// Gone from the list, so no more pushes to it.
	if got := devicesList(e); len(got) != 0 {
		t.Fatalf("devices after delete: %+v, want none", got)
	}
	e.push.Reset()
	st, body = e.admin(http.MethodPost, "/geo-rules", model.GeoRuleBody{Country: "KP"})
	mustStatus(t, st, http.StatusCreated, body)

	// History still names the phone.
	st, body = e.admin(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusOK, body)
	hist := decode[model.HistoryResponse](t, body)
	if len(hist.Items) != 1 || hist.Items[0].DecidedByDevice == nil || *hist.Items[0].DecidedByDevice != "Pixel 8" {
		t.Fatalf("history after device delete: %s", body)
	}

	// The same token registering again comes back with the same id.
	st, body = e.admin(http.MethodPost, "/devices", devicesBody("fcm-pixel-8", model.PlatformAndroid, strp("Pixel 8")))
	mustStatus(t, st, http.StatusOK, body)
	if back := decode[model.DeviceItem](t, body); back.ID != dev.ID {
		t.Fatalf("re-registered id %s, want %s", back.ID, dev.ID)
	}
	if got := devicesList(e); len(got) != 1 || got[0].ID != dev.ID {
		t.Fatalf("devices after re-registration: %+v", got)
	}
}
