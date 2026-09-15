package dbtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Conformance runs every method of db.Store against the semantics written
// on the interface. open returns a fresh, empty store for each subtest. The
// in-memory store and the PostgreSQL store both run it, so the handler and
// rules tests (which use the in-memory store) exercise the behaviour of
// production.
//
// Times are whole seconds: PostgreSQL keeps microseconds, and every returned
// time must be in UTC (checkTime verifies both).
func Conformance(t *testing.T, open func(t *testing.T) db.Store) {
	t.Helper()
	t.Run("Ping", func(t *testing.T) {
		if err := open(t).Ping(context.Background()); err != nil {
			t.Fatalf("ping: %v", err)
		}
	})
	t.Run("Servers", func(t *testing.T) { conformServers(t, open) })
	t.Run("Devices", func(t *testing.T) { conformDevices(t, open) })
	t.Run("Requests", func(t *testing.T) { conformRequests(t, open) })
	t.Run("Whitelist", func(t *testing.T) { conformWhitelist(t, open) })
	t.Run("BlockedIPs", func(t *testing.T) { conformBlockedIPs(t, open) })
	t.Run("GeoRules", func(t *testing.T) { conformGeoRules(t, open) })
}

// base is the reference instant of every subtest.
var base = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

const (
	ipA = "203.0.113.42"
	ipB = "198.51.100.7"
)

// Helpers

func str(s string) *string { return &s }

func at(d time.Duration) time.Time { return base.Add(d) }

func atPtr(d time.Duration) *time.Time {
	t := at(d)
	return &t
}

func checkErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: got error %v, want %v", what, err, want)
	}
}

func checkTime(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("%s is in %v, want UTC", what, got.Location())
	}
}

func checkTimePtr(t *testing.T, what string, got, want *time.Time) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Fatalf("%s = %v, want %v", what, got, want)
	default:
		checkTime(t, what, *got, *want)
	}
}

func checkStrPtr(t *testing.T, what string, got, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Fatalf("%s = nil, want %q", what, *want)
	case want == nil:
		t.Fatalf("%s = %q, want nil", what, *got)
	case *got != *want:
		t.Fatalf("%s = %q, want %q", what, *got, *want)
	}
}

func checkIDs(t *testing.T, what string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d rows %v, want %d %v", what, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: row %d is %s, want %s (got %v, want %v)", what, i, got[i], want[i], got, want)
		}
	}
}

func mustServer(t *testing.T, s db.Store, name string) *model.Server {
	t.Helper()
	srv, err := s.CreateServer(context.Background(), name, "hash-"+name, model.OSLinux, base)
	if err != nil {
		t.Fatalf("create server %s: %v", name, err)
	}
	return srv
}

func mustDevice(t *testing.T, s db.Store, token, label string) *model.Device {
	t.Helper()
	d, _, err := s.UpsertDevice(context.Background(), token, model.PlatformAndroid, str(label), base)
	if err != nil {
		t.Fatalf("upsert device %s: %v", token, err)
	}
	return d
}

// pendingRequest builds an ssh request from ipA, pending, created at when.
func pendingRequest(srv *model.Server, username string, when time.Time) *model.Request {
	return &model.Request{
		ServerID:  srv.ID,
		Context:   model.ContextSSH,
		Username:  username,
		SourceIP:  str(ipA),
		Hostname:  srv.Name,
		Status:    model.StatusPending,
		CreatedAt: when,
		ExpiresAt: when.Add(30 * time.Second),
	}
}

func mustRequest(t *testing.T, s db.Store, r *model.Request) *model.Request {
	t.Helper()
	got, err := s.CreateRequest(context.Background(), r)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	return got
}

func mustWhitelist(t *testing.T, s db.Store, e *model.WhitelistEntry, now time.Time) *model.WhitelistEntry {
	t.Helper()
	got, _, err := s.UpsertWhitelist(context.Background(), e, now)
	if err != nil {
		t.Fatalf("upsert whitelist %s/%s: %v", e.Username, e.Context, err)
	}
	return got
}

func mustBlock(t *testing.T, s db.Store, b *model.BlockedIP) *model.BlockedIP {
	t.Helper()
	got, err := s.UpsertBlockedIP(context.Background(), b)
	if err != nil {
		t.Fatalf("upsert blocked ip %s: %v", b.IP, err)
	}
	return got
}

// Servers

func conformServers(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()

	t.Run("Create", func(t *testing.T) {
		s := open(t)
		srv, err := s.CreateServer(ctx, "web-01", "hash-1", model.OSLinux, base)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if srv.ID == "" {
			t.Fatal("create: empty id")
		}
		if srv.Name != "web-01" || srv.TokenHash != "hash-1" || srv.OS != model.OSLinux {
			t.Fatalf("create: got %+v", srv)
		}
		checkTime(t, "created_at", srv.CreatedAt, base)
		checkTimePtr(t, "last_seen_at", srv.LastSeenAt, nil)
	})

	t.Run("ConflictOnName", func(t *testing.T) {
		s := open(t)
		mustServer(t, s, "web-01")
		_, err := s.CreateServer(ctx, "web-01", "other-hash", model.OSLinux, base)
		checkErr(t, "duplicate name", err, db.ErrConflict)
	})

	t.Run("ConflictOnHash", func(t *testing.T) {
		s := open(t)
		mustServer(t, s, "web-01")
		_, err := s.CreateServer(ctx, "web-02", "hash-web-01", model.OSWindows, base)
		checkErr(t, "duplicate hash", err, db.ErrConflict)
	})

	t.Run("ByHashAndByName", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		mustServer(t, s, "db-01")

		byHash, err := s.ServerByTokenHash(ctx, "hash-web-01")
		if err != nil {
			t.Fatalf("by hash: %v", err)
		}
		if byHash.ID != srv.ID || byHash.Name != "web-01" {
			t.Fatalf("by hash: got %+v", byHash)
		}
		checkTime(t, "created_at", byHash.CreatedAt, base)

		byName, err := s.ServerByName(ctx, "web-01")
		if err != nil {
			t.Fatalf("by name: %v", err)
		}
		if byName.ID != srv.ID || byName.TokenHash != "hash-web-01" {
			t.Fatalf("by name: got %+v", byName)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		s := open(t)
		mustServer(t, s, "web-01")
		_, err := s.ServerByTokenHash(ctx, "unknown")
		checkErr(t, "by hash", err, db.ErrNotFound)
		_, err = s.ServerByName(ctx, "unknown")
		checkErr(t, "by name", err, db.ErrNotFound)
		checkErr(t, "touch", s.TouchServer(ctx, NewID(), base), db.ErrNotFound)
	})

	t.Run("Touch", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		if err := s.TouchServer(ctx, srv.ID, at(time.Minute)); err != nil {
			t.Fatalf("touch: %v", err)
		}
		got, err := s.ServerByName(ctx, "web-01")
		if err != nil {
			t.Fatalf("by name: %v", err)
		}
		checkTimePtr(t, "last_seen_at", got.LastSeenAt, atPtr(time.Minute))
	})
}

// Devices

func conformDevices(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()

	t.Run("UpsertCreatesThenRefreshes", func(t *testing.T) {
		s := open(t)
		d1, created, err := s.UpsertDevice(ctx, "tok-1", model.PlatformAndroid, str("Pixel 8"), base)
		if err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		if !created || d1.ID == "" {
			t.Fatalf("first upsert: created=%v id=%q", created, d1.ID)
		}
		if d1.FCMToken != "tok-1" || d1.Platform != model.PlatformAndroid {
			t.Fatalf("first upsert: got %+v", d1)
		}
		checkStrPtr(t, "label", d1.Label, str("Pixel 8"))
		checkTime(t, "created_at", d1.CreatedAt, base)
		checkTimePtr(t, "last_seen_at", d1.LastSeenAt, atPtr(0))

		d2, created, err := s.UpsertDevice(ctx, "tok-1", model.PlatformIOS, str("iPhone"), at(time.Hour))
		if err != nil {
			t.Fatalf("second upsert: %v", err)
		}
		if created {
			t.Fatal("second upsert: created=true, want false")
		}
		if d2.ID != d1.ID || d2.Platform != model.PlatformIOS {
			t.Fatalf("second upsert: got %+v, want id %s platform ios", d2, d1.ID)
		}
		checkStrPtr(t, "label", d2.Label, str("iPhone"))
		checkTime(t, "created_at", d2.CreatedAt, base)
		checkTimePtr(t, "last_seen_at", d2.LastSeenAt, atPtr(time.Hour))

		d3, created, err := s.UpsertDevice(ctx, "tok-1", model.PlatformIOS, nil, at(2*time.Hour))
		if err != nil {
			t.Fatalf("third upsert: %v", err)
		}
		if created || d3.ID != d1.ID {
			t.Fatalf("third upsert: created=%v id=%s", created, d3.ID)
		}
		checkStrPtr(t, "label", d3.Label, nil)
	})

	t.Run("ByID", func(t *testing.T) {
		s := open(t)
		d := mustDevice(t, s, "tok-1", "Pixel 8")
		got, err := s.DeviceByID(ctx, d.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		if got.ID != d.ID || got.FCMToken != "tok-1" || got.Platform != model.PlatformAndroid {
			t.Fatalf("by id: got %+v", got)
		}
		checkStrPtr(t, "label", got.Label, str("Pixel 8"))
		checkTime(t, "created_at", got.CreatedAt, base)
		checkTimePtr(t, "last_seen_at", got.LastSeenAt, atPtr(0))

		_, err = s.DeviceByID(ctx, NewID())
		checkErr(t, "unknown id", err, db.ErrNotFound)
	})

	t.Run("ListOrderedByCreation", func(t *testing.T) {
		s := open(t)
		c, _, err := s.UpsertDevice(ctx, "tok-c", model.PlatformAndroid, nil, at(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		a, _, err := s.UpsertDevice(ctx, "tok-a", model.PlatformAndroid, nil, base)
		if err != nil {
			t.Fatal(err)
		}
		b, _, err := s.UpsertDevice(ctx, "tok-b", model.PlatformIOS, nil, at(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		list, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		checkIDs(t, "list", deviceIDs(list), []string{a.ID, b.ID, c.ID})
		checkTime(t, "created_at", list[0].CreatedAt, base)
	})

	t.Run("Delete", func(t *testing.T) {
		s := open(t)
		d := mustDevice(t, s, "tok-1", "Pixel 8")
		if err := s.DeleteDevice(ctx, d.ID, at(time.Minute)); err != nil {
			t.Fatalf("delete: %v", err)
		}
		_, err := s.DeviceByID(ctx, d.ID)
		checkErr(t, "by id after delete", err, db.ErrNotFound)
		checkErr(t, "second delete", s.DeleteDevice(ctx, d.ID, at(2*time.Minute)), db.ErrNotFound)
		checkErr(t, "unknown id", s.DeleteDevice(ctx, NewID(), at(time.Minute)), db.ErrNotFound)
		list, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("list after delete: %d rows, want 0", len(list))
		}
	})

	// The same FCM token registering again brings the phone back, with the
	// same id, and it is listed and pushed to again.
	t.Run("DeleteThenRegisterAgain", func(t *testing.T) {
		s := open(t)
		d := mustDevice(t, s, "tok-1", "Pixel 8")
		if err := s.DeleteDevice(ctx, d.ID, at(time.Minute)); err != nil {
			t.Fatalf("delete: %v", err)
		}
		back, created, err := s.UpsertDevice(ctx, "tok-1", model.PlatformAndroid, str("Pixel 8 again"), at(time.Hour))
		if err != nil {
			t.Fatalf("upsert after delete: %v", err)
		}
		if created || back.ID != d.ID || back.DeletedAt != nil {
			t.Fatalf("upsert after delete: created=%v id=%s deleted_at=%v, want same id, not deleted", created, back.ID, back.DeletedAt)
		}
		if _, err := s.DeviceByID(ctx, d.ID); err != nil {
			t.Fatalf("by id after re-registration: %v", err)
		}
		list, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		checkIDs(t, "list", deviceIDs(list), []string{d.ID})
		checkStrPtr(t, "label", list[0].Label, str("Pixel 8 again"))
	})

	// Deleting a phone must not touch history: the request and the whitelist
	// entry it decided keep the device id and its label (contract v1, DELETE
	// /devices: "History rows keep the label").
	t.Run("DeleteKeepsReferencesAndLabel", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		dev := mustDevice(t, s, "tok-1", "Pixel 8")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base))
		if _, err := s.DecideRequest(ctx, r.ID, model.StatusApproved, &dev.ID, at(5*time.Second)); err != nil {
			t.Fatalf("decide: %v", err)
		}
		w := mustWhitelist(t, s, &model.WhitelistEntry{
			Username: "deploy", Context: model.ContextSSH, CreatedFrom: &r.ID, CreatedByDevice: &dev.ID,
		}, base)

		if err := s.DeleteDevice(ctx, dev.ID, at(time.Minute)); err != nil {
			t.Fatalf("delete: %v", err)
		}

		gotR, err := s.RequestByID(ctx, r.ID)
		if err != nil {
			t.Fatalf("request by id: %v", err)
		}
		if gotR.Status != model.StatusApproved {
			t.Fatalf("request status after device delete = %s, want approved", gotR.Status)
		}
		checkStrPtr(t, "decided_by", gotR.DecidedBy, str(model.DecidedByAdmin))
		checkStrPtr(t, "decided_by_device", gotR.DecidedByDevice, &dev.ID)
		checkStrPtr(t, "decided_by_device label", gotR.DecidedByDeviceLabel, str("Pixel 8"))
		checkTimePtr(t, "decided_at", gotR.DecidedAt, atPtr(5*time.Second))

		gotW, err := s.WhitelistByID(ctx, w.ID)
		if err != nil {
			t.Fatalf("whitelist by id: %v", err)
		}
		checkStrPtr(t, "created_by_device", gotW.CreatedByDevice, &dev.ID)
		checkStrPtr(t, "created_by_device label", gotW.CreatedByDeviceLabel, str("Pixel 8"))
		checkStrPtr(t, "created_from", gotW.CreatedFrom, &r.ID)
	})
}

func deviceIDs(list []model.Device) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.ID
	}
	return out
}

// Requests

func conformRequests(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()

	t.Run("CreatePending", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		in := pendingRequest(srv, "deploy", base)
		in.TTY = str("pts/0")
		in.Command = str("sudo systemctl restart nginx")
		in.Geo = &model.Geo{Country: "FR", City: "Paris", ASN: "AS12876"}

		got := mustRequest(t, s, in)
		if got.ID == "" {
			t.Fatal("create: empty id")
		}
		check := func(r *model.Request) {
			t.Helper()
			if r.ServerID != srv.ID || r.ServerName != "web-01" {
				t.Fatalf("server: got %q/%q, want %q/web-01", r.ServerID, r.ServerName, srv.ID)
			}
			if r.Context != model.ContextSSH || r.Username != "deploy" || r.Hostname != "web-01" {
				t.Fatalf("fields: got %+v", r)
			}
			checkStrPtr(t, "source_ip", r.SourceIP, str(ipA))
			checkStrPtr(t, "tty", r.TTY, str("pts/0"))
			checkStrPtr(t, "command", r.Command, str("sudo systemctl restart nginx"))
			if r.Geo == nil || *r.Geo != *in.Geo {
				t.Fatalf("geo = %+v, want %+v", r.Geo, in.Geo)
			}
			if r.Status != model.StatusPending {
				t.Fatalf("status = %s, want pending", r.Status)
			}
			checkStrPtr(t, "decided_by", r.DecidedBy, nil)
			checkStrPtr(t, "decided_by_device", r.DecidedByDevice, nil)
			checkStrPtr(t, "decided_by_device label", r.DecidedByDeviceLabel, nil)
			checkTime(t, "created_at", r.CreatedAt, base)
			checkTime(t, "expires_at", r.ExpiresAt, at(30*time.Second))
			checkTimePtr(t, "decided_at", r.DecidedAt, nil)
		}
		check(got)

		again, err := s.RequestByID(ctx, got.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		if again.ID != got.ID {
			t.Fatalf("by id: got %s, want %s", again.ID, got.ID)
		}
		check(again)
	})

	// Requests decided by the rules are final from the start, decision
	// columns included; the store keeps them as given.
	t.Run("CreateFinalStatuses", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		cases := []struct {
			status    string
			decidedBy *string
			decidedAt *time.Time
		}{
			{model.StatusWhitelisted, str(model.DecidedByWhitelist), atPtr(0)},
			{model.StatusBlockedIP, str(model.DecidedByAutoblock), atPtr(0)},
			{model.StatusBlockedGeo, str(model.DecidedByGeoRule), atPtr(0)},
			{model.StatusNotified, nil, nil},
			{model.StatusTimeout, str(model.DecidedByTimeout), nil},
			{model.StatusDenied, str(model.DecidedByAdmin), atPtr(7 * time.Second)},
			{model.StatusApproved, str(model.DecidedByAdmin), atPtr(7 * time.Second)},
		}
		for _, c := range cases {
			in := pendingRequest(srv, "deploy", base)
			in.Status, in.DecidedBy, in.DecidedAt = c.status, c.decidedBy, c.decidedAt
			got := mustRequest(t, s, in)
			if got.Status != c.status {
				t.Fatalf("%s: status = %s", c.status, got.Status)
			}
			checkStrPtr(t, c.status+" decided_by", got.DecidedBy, c.decidedBy)
			checkTimePtr(t, c.status+" decided_at", got.DecidedAt, c.decidedAt)
		}

		// A sudo request has no source address and no geo.
		in := pendingRequest(srv, "deploy", base)
		in.Context, in.SourceIP, in.Geo = model.ContextSudo, nil, nil
		in.Command = str("sudo ls")
		got := mustRequest(t, s, in)
		if got.Context != model.ContextSudo || got.SourceIP != nil || got.Geo != nil {
			t.Fatalf("sudo request: got %+v", got)
		}
	})

	t.Run("CreateEmptyStatusIsPending", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		in := pendingRequest(srv, "deploy", base)
		in.Status = ""
		if got := mustRequest(t, s, in); got.Status != model.StatusPending {
			t.Fatalf("status = %q, want pending", got.Status)
		}
	})

	t.Run("CreateUnknownServer", func(t *testing.T) {
		s := open(t)
		in := pendingRequest(&model.Server{ID: NewID(), Name: "ghost"}, "deploy", base)
		_, err := s.CreateRequest(ctx, in)
		checkErr(t, "create", err, db.ErrNotFound)
	})

	t.Run("ByIDNotFound", func(t *testing.T) {
		s := open(t)
		_, err := s.RequestByID(ctx, NewID())
		checkErr(t, "by id", err, db.ErrNotFound)
	})

	t.Run("DecideApprove", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		dev := mustDevice(t, s, "tok-1", "Pixel 8")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base))

		got, err := s.DecideRequest(ctx, r.ID, model.StatusApproved, &dev.ID, at(5*time.Second))
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		check := func(r *model.Request) {
			t.Helper()
			if r.Status != model.StatusApproved || r.ServerName != "web-01" {
				t.Fatalf("decided: got %+v", r)
			}
			checkStrPtr(t, "decided_by", r.DecidedBy, str(model.DecidedByAdmin))
			checkStrPtr(t, "decided_by_device", r.DecidedByDevice, &dev.ID)
			checkStrPtr(t, "decided_by_device label", r.DecidedByDeviceLabel, str("Pixel 8"))
			checkTimePtr(t, "decided_at", r.DecidedAt, atPtr(5*time.Second))
		}
		check(got)
		again, err := s.RequestByID(ctx, r.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		check(again)

		// Second verdict: first one wins, the row does not change.
		_, err = s.DecideRequest(ctx, r.ID, model.StatusDenied, nil, at(6*time.Second))
		checkErr(t, "second decide", err, db.ErrAlreadyDecided)
		again, err = s.RequestByID(ctx, r.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		check(again)
	})

	t.Run("DecideDenyWithoutDevice", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base))
		got, err := s.DecideRequest(ctx, r.ID, model.StatusDenied, nil, at(5*time.Second))
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		if got.Status != model.StatusDenied {
			t.Fatalf("status = %s, want denied", got.Status)
		}
		checkStrPtr(t, "decided_by", got.DecidedBy, str(model.DecidedByAdmin))
		checkStrPtr(t, "decided_by_device", got.DecidedByDevice, nil)
		checkStrPtr(t, "decided_by_device label", got.DecidedByDeviceLabel, nil)
		checkTimePtr(t, "decided_at", got.DecidedAt, atPtr(5*time.Second))
	})

	t.Run("DecideAfterExpiry", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base)) // expires at base + 30 s
		_, err := s.DecideRequest(ctx, r.ID, model.StatusApproved, nil, at(30*time.Second))
		checkErr(t, "decide at expiry", err, db.ErrAlreadyDecided)
		_, err = s.DecideRequest(ctx, r.ID, model.StatusApproved, nil, at(31*time.Second))
		checkErr(t, "decide after expiry", err, db.ErrAlreadyDecided)
		got, err := s.RequestByID(ctx, r.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		if got.Status != model.StatusPending || got.DecidedBy != nil {
			t.Fatalf("row changed by a refused decide: %+v", got)
		}
	})

	t.Run("DecideUnknown", func(t *testing.T) {
		s := open(t)
		_, err := s.DecideRequest(ctx, NewID(), model.StatusApproved, nil, base)
		checkErr(t, "decide", err, db.ErrNotFound)
	})

	t.Run("DecideAfterTimeout", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base))
		if _, err := s.MarkTimeout(ctx, r.ID, at(30*time.Second)); err != nil {
			t.Fatalf("mark timeout: %v", err)
		}
		_, err := s.DecideRequest(ctx, r.ID, model.StatusApproved, nil, at(10*time.Second))
		checkErr(t, "decide", err, db.ErrAlreadyDecided)
	})

	t.Run("MarkTimeout", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		r := mustRequest(t, s, pendingRequest(srv, "deploy", base))

		ok, err := s.MarkTimeout(ctx, r.ID, at(30*time.Second))
		if err != nil || !ok {
			t.Fatalf("mark timeout: ok=%v err=%v, want true", ok, err)
		}
		got, err := s.RequestByID(ctx, r.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		if got.Status != model.StatusTimeout {
			t.Fatalf("status = %s, want timeout", got.Status)
		}
		checkStrPtr(t, "decided_by", got.DecidedBy, str(model.DecidedByTimeout))
		checkStrPtr(t, "decided_by_device", got.DecidedByDevice, nil)
		checkTimePtr(t, "decided_at", got.DecidedAt, nil)

		ok, err = s.MarkTimeout(ctx, r.ID, at(31*time.Second))
		if err != nil || ok {
			t.Fatalf("second mark timeout: ok=%v err=%v, want false", ok, err)
		}

		decided := mustRequest(t, s, pendingRequest(srv, "deploy", base))
		if _, err := s.DecideRequest(ctx, decided.ID, model.StatusApproved, nil, at(time.Second)); err != nil {
			t.Fatalf("decide: %v", err)
		}
		ok, err = s.MarkTimeout(ctx, decided.ID, at(30*time.Second))
		if err != nil || ok {
			t.Fatalf("mark timeout on a decided row: ok=%v err=%v, want false", ok, err)
		}

		_, err = s.MarkTimeout(ctx, NewID(), base)
		checkErr(t, "unknown id", err, db.ErrNotFound)
	})

	t.Run("List", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		dbs := mustServer(t, s, "db-01")

		r1 := pendingRequest(web, "alice", at(1*time.Minute))
		r2 := pendingRequest(dbs, "bob", at(2*time.Minute))
		r2.Context, r2.Status, r2.DecidedBy, r2.DecidedAt = model.ContextSudo, model.StatusDenied, str(model.DecidedByAdmin), atPtr(2*time.Minute+5*time.Second)
		r3 := pendingRequest(web, "alice", at(3*time.Minute))
		r3.Context, r3.Status, r3.DecidedBy, r3.DecidedAt = model.ContextSudo, model.StatusApproved, str(model.DecidedByAdmin), atPtr(3*time.Minute+5*time.Second)
		r4 := pendingRequest(dbs, "alice", at(4*time.Minute))
		r4.Status, r4.DecidedBy = model.StatusTimeout, str(model.DecidedByTimeout)
		// Inserted out of order on purpose: the order must come from created_at.
		id3 := mustRequest(t, s, r3).ID
		id1 := mustRequest(t, s, r1).ID
		id4 := mustRequest(t, s, r4).ID
		id2 := mustRequest(t, s, r2).ID

		list := func(what string, f db.RequestFilter, want ...string) []model.Request {
			t.Helper()
			got, err := s.ListRequests(ctx, f)
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			checkIDs(t, what, requestIDs(got), want)
			return got
		}
		all := list("all", db.RequestFilter{Limit: 10}, id4, id3, id2, id1)
		if all[2].ServerName != "db-01" || all[3].ServerName != "web-01" {
			t.Fatalf("server names not joined: %q, %q", all[2].ServerName, all[3].ServerName)
		}
		checkTime(t, "created_at", all[0].CreatedAt, at(4*time.Minute))
		list("no limit", db.RequestFilter{}, id4, id3, id2, id1)
		list("limit 2", db.RequestFilter{Limit: 2}, id4, id3)
		list("before", db.RequestFilter{Limit: 10, Before: atPtr(3 * time.Minute)}, id2, id1)
		list("before + limit", db.RequestFilter{Limit: 1, Before: atPtr(3 * time.Minute)}, id2)
		list("server", db.RequestFilter{Limit: 10, Server: "web-01"}, id3, id1)
		list("username", db.RequestFilter{Limit: 10, Username: "alice"}, id4, id3, id1)
		list("context", db.RequestFilter{Limit: 10, Context: model.ContextSudo}, id3, id2)
		list("status", db.RequestFilter{Limit: 10, Status: model.StatusDenied}, id2)
		list("username + context", db.RequestFilter{Limit: 10, Username: "alice", Context: model.ContextSSH}, id4, id1)
		list("unknown server", db.RequestFilter{Limit: 10, Server: "ghost"})
	})

	t.Run("CountDenials", func(t *testing.T) {
		s := open(t)
		srv := mustServer(t, s, "web-01")
		denied := func(ip string, decidedAt time.Duration) {
			r := pendingRequest(srv, "deploy", at(decidedAt-5*time.Second))
			r.SourceIP = str(ip)
			r.Status, r.DecidedBy, r.DecidedAt = model.StatusDenied, str(model.DecidedByAdmin), atPtr(decidedAt)
			mustRequest(t, s, r)
		}
		denied(ipA, -time.Second)                                   // before the window
		denied(ipA, 0)                                              // at the window start: counted
		denied(ipA, 10*time.Minute)                                 // counted
		denied(ipA, 20*time.Minute)                                 // counted
		denied(ipB, 5*time.Minute)                                  // other address
		timeout := pendingRequest(srv, "deploy", at(5*time.Minute)) // timeouts never count
		timeout.Status, timeout.DecidedBy = model.StatusTimeout, str(model.DecidedByTimeout)
		mustRequest(t, s, timeout)
		approved := pendingRequest(srv, "deploy", at(6*time.Minute))
		approved.Status, approved.DecidedBy, approved.DecidedAt = model.StatusApproved, str(model.DecidedByAdmin), atPtr(6*time.Minute)
		mustRequest(t, s, approved)

		st, err := s.CountDenials(ctx, ipA, base)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if st.Count != 3 {
			t.Fatalf("count = %d, want 3", st.Count)
		}
		checkTime(t, "first", st.First, base)
		checkTime(t, "last", st.Last, at(20*time.Minute))

		st, err = s.CountDenials(ctx, ipA, at(21*time.Minute))
		if err != nil {
			t.Fatalf("count outside window: %v", err)
		}
		if st.Count != 0 || !st.First.IsZero() || !st.Last.IsZero() {
			t.Fatalf("count outside window = %+v, want 0 and zero times", st)
		}

		st, err = s.CountDenials(ctx, "192.0.2.1", base)
		if err != nil {
			t.Fatalf("count unknown ip: %v", err)
		}
		if st.Count != 0 {
			t.Fatalf("count unknown ip = %d, want 0", st.Count)
		}
	})
}

func requestIDs(list []model.Request) []string {
	out := make([]string, len(list))
	for i, r := range list {
		out[i] = r.ID
	}
	return out
}

// Whitelist

func conformWhitelist(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()
	entry := func(username, context string, serverID *string, expires *time.Time) *model.WhitelistEntry {
		return &model.WhitelistEntry{Username: username, Context: context, ServerID: serverID, ExpiresAt: expires}
	}

	t.Run("FindPrefersServerEntry", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		dbs := mustServer(t, s, "db-01")
		global := mustWhitelist(t, s, entry("deploy", model.ContextSSH, nil, nil), base)
		perServer := mustWhitelist(t, s, entry("deploy", model.ContextSSH, &web.ID, nil), base)

		got, err := s.FindWhitelist(ctx, "deploy", model.ContextSSH, web.ID, base)
		if err != nil {
			t.Fatalf("find on web-01: %v", err)
		}
		if got.ID != perServer.ID {
			t.Fatalf("find on web-01 = %s, want the per-server entry %s", got.ID, perServer.ID)
		}
		checkStrPtr(t, "server_id", got.ServerID, &web.ID)
		checkStrPtr(t, "server name", got.ServerName, str("web-01"))

		got, err = s.FindWhitelist(ctx, "deploy", model.ContextSSH, dbs.ID, base)
		if err != nil {
			t.Fatalf("find on db-01: %v", err)
		}
		if got.ID != global.ID {
			t.Fatalf("find on db-01 = %s, want the global entry %s", got.ID, global.ID)
		}
		checkStrPtr(t, "server_id", got.ServerID, nil)
		checkStrPtr(t, "server name", got.ServerName, nil)
		checkTime(t, "created_at", got.CreatedAt, base)
	})

	t.Run("FindIgnoresExpiredAndOtherContext", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		ssh := mustWhitelist(t, s, entry("deploy", model.ContextSSH, &web.ID, atPtr(time.Hour)), base)
		sudo := mustWhitelist(t, s, entry("deploy", model.ContextSudo, &web.ID, nil), base)

		got, err := s.FindWhitelist(ctx, "deploy", model.ContextSSH, web.ID, at(59*time.Minute))
		if err != nil || got.ID != ssh.ID {
			t.Fatalf("find before expiry: %v %v", got, err)
		}
		checkTimePtr(t, "expires_at", got.ExpiresAt, atPtr(time.Hour))
		_, err = s.FindWhitelist(ctx, "deploy", model.ContextSSH, web.ID, at(time.Hour))
		checkErr(t, "find at expiry", err, db.ErrNotFound)
		_, err = s.FindWhitelist(ctx, "deploy", model.ContextSSH, web.ID, at(2*time.Hour))
		checkErr(t, "find after expiry", err, db.ErrNotFound)

		got, err = s.FindWhitelist(ctx, "deploy", model.ContextSudo, web.ID, at(2*time.Hour))
		if err != nil || got.ID != sudo.ID {
			t.Fatalf("find sudo: %v %v", got, err)
		}
		_, err = s.FindWhitelist(ctx, "someone", model.ContextSudo, web.ID, base)
		checkErr(t, "find unknown user", err, db.ErrNotFound)
	})

	t.Run("ListForAdminAndForServer", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		dbs := mustServer(t, s, "db-01")
		g := mustWhitelist(t, s, entry("ansible", model.ContextSSH, nil, nil), at(1*time.Minute))
		a := mustWhitelist(t, s, entry("deploy", model.ContextSSH, &web.ID, nil), at(2*time.Minute))
		b := mustWhitelist(t, s, entry("deploy", model.ContextSudo, &dbs.ID, nil), at(3*time.Minute))

		all, err := s.ListWhitelist(ctx, nil, at(10*time.Minute))
		if err != nil {
			t.Fatalf("list all: %v", err)
		}
		checkIDs(t, "list all", whitelistIDs(all), []string{b.ID, a.ID, g.ID})
		checkStrPtr(t, "server name", all[1].ServerName, str("web-01"))
		checkStrPtr(t, "global server name", all[2].ServerName, nil)
		checkTime(t, "created_at", all[0].CreatedAt, at(3*time.Minute))

		forWeb, err := s.ListWhitelist(ctx, &web.ID, at(10*time.Minute))
		if err != nil {
			t.Fatalf("list web-01: %v", err)
		}
		checkIDs(t, "list web-01", whitelistIDs(forWeb), []string{a.ID, g.ID})

		forDB, err := s.ListWhitelist(ctx, &dbs.ID, at(10*time.Minute))
		if err != nil {
			t.Fatalf("list db-01: %v", err)
		}
		checkIDs(t, "list db-01", whitelistIDs(forDB), []string{b.ID, g.ID})
	})

	t.Run("ListDeletesExpiredRows", func(t *testing.T) {
		s := open(t)
		keep := mustWhitelist(t, s, entry("ansible", model.ContextSSH, nil, nil), base)
		gone := mustWhitelist(t, s, entry("deploy", model.ContextSSH, nil, atPtr(time.Hour)), at(time.Minute))

		before, err := s.ListWhitelist(ctx, nil, at(59*time.Minute))
		if err != nil {
			t.Fatalf("list before expiry: %v", err)
		}
		checkIDs(t, "list before expiry", whitelistIDs(before), []string{gone.ID, keep.ID})

		after, err := s.ListWhitelist(ctx, nil, at(time.Hour))
		if err != nil {
			t.Fatalf("list at expiry: %v", err)
		}
		checkIDs(t, "list at expiry", whitelistIDs(after), []string{keep.ID})
		_, err = s.WhitelistByID(ctx, gone.ID)
		checkErr(t, "expired row after list", err, db.ErrNotFound)
		if _, err := s.WhitelistByID(ctx, keep.ID); err != nil {
			t.Fatalf("permanent row after list: %v", err)
		}
	})

	t.Run("UpsertCreatesThenRefreshes", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		dev := mustDevice(t, s, "tok-1", "Pixel 8")
		r := mustRequest(t, s, pendingRequest(web, "deploy", base))

		e1, created, err := s.UpsertWhitelist(ctx, &model.WhitelistEntry{
			Username: "deploy", Context: model.ContextSSH, ServerID: &web.ID,
			ExpiresAt: atPtr(time.Hour), CreatedFrom: &r.ID, CreatedByDevice: &dev.ID,
		}, base)
		if err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		if !created || e1.ID == "" {
			t.Fatalf("first upsert: created=%v id=%q", created, e1.ID)
		}
		if e1.Username != "deploy" || e1.Context != model.ContextSSH {
			t.Fatalf("first upsert: got %+v", e1)
		}
		checkStrPtr(t, "server_id", e1.ServerID, &web.ID)
		checkStrPtr(t, "server name", e1.ServerName, str("web-01"))
		checkTimePtr(t, "expires_at", e1.ExpiresAt, atPtr(time.Hour))
		checkTime(t, "created_at", e1.CreatedAt, base)
		checkStrPtr(t, "created_from", e1.CreatedFrom, &r.ID)
		checkStrPtr(t, "created_by_device", e1.CreatedByDevice, &dev.ID)
		checkStrPtr(t, "created_by_device label", e1.CreatedByDeviceLabel, str("Pixel 8"))

		// Same key again: only expires_at moves, the origin of the entry stays.
		e2, created, err := s.UpsertWhitelist(ctx, &model.WhitelistEntry{
			Username: "deploy", Context: model.ContextSSH, ServerID: &web.ID, ExpiresAt: nil,
		}, at(time.Hour))
		if err != nil {
			t.Fatalf("second upsert: %v", err)
		}
		if created || e2.ID != e1.ID {
			t.Fatalf("second upsert: created=%v id=%s, want false and %s", created, e2.ID, e1.ID)
		}
		checkTimePtr(t, "expires_at", e2.ExpiresAt, nil)
		checkTime(t, "created_at", e2.CreatedAt, base)
		checkStrPtr(t, "created_from", e2.CreatedFrom, &r.ID)
		checkStrPtr(t, "created_by_device", e2.CreatedByDevice, &dev.ID)
		checkStrPtr(t, "created_by_device label", e2.CreatedByDeviceLabel, str("Pixel 8"))

		e3, created, err := s.UpsertWhitelist(ctx, &model.WhitelistEntry{
			Username: "deploy", Context: model.ContextSSH, ServerID: &web.ID, ExpiresAt: atPtr(3 * time.Hour),
		}, at(2*time.Hour))
		if err != nil {
			t.Fatalf("third upsert: %v", err)
		}
		if created || e3.ID != e1.ID {
			t.Fatalf("third upsert: created=%v id=%s", created, e3.ID)
		}
		checkTimePtr(t, "expires_at", e3.ExpiresAt, atPtr(3*time.Hour))
	})

	t.Run("UpsertGlobalAndServerAreIndependent", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		g, created, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSSH, nil, nil), base)
		if err != nil || !created {
			t.Fatalf("global: created=%v err=%v", created, err)
		}
		a, created, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSSH, &web.ID, nil), base)
		if err != nil || !created {
			t.Fatalf("per-server: created=%v err=%v", created, err)
		}
		if g.ID == a.ID {
			t.Fatal("global and per-server entries share an id")
		}
		g2, created, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSSH, nil, atPtr(time.Hour)), base)
		if err != nil || created || g2.ID != g.ID {
			t.Fatalf("global again: created=%v id=%s err=%v, want %s", created, g2.ID, err, g.ID)
		}
		a2, created, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSSH, &web.ID, atPtr(time.Hour)), base)
		if err != nil || created || a2.ID != a.ID {
			t.Fatalf("per-server again: created=%v id=%s err=%v, want %s", created, a2.ID, err, a.ID)
		}
		sudo, created, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSudo, nil, nil), base)
		if err != nil || !created || sudo.ID == g.ID {
			t.Fatalf("other context: created=%v id=%s err=%v", created, sudo.ID, err)
		}
	})

	t.Run("UpsertUnknownServer", func(t *testing.T) {
		s := open(t)
		_, _, err := s.UpsertWhitelist(ctx, entry("deploy", model.ContextSSH, str(NewID()), nil), base)
		checkErr(t, "upsert", err, db.ErrNotFound)
	})

	t.Run("ByIDAndDelete", func(t *testing.T) {
		s := open(t)
		web := mustServer(t, s, "web-01")
		e := mustWhitelist(t, s, entry("deploy", model.ContextSudo, &web.ID, atPtr(time.Hour)), base)

		got, err := s.WhitelistByID(ctx, e.ID)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		if got.ID != e.ID || got.Username != "deploy" || got.Context != model.ContextSudo {
			t.Fatalf("by id: got %+v", got)
		}
		checkStrPtr(t, "server name", got.ServerName, str("web-01"))
		checkTimePtr(t, "expires_at", got.ExpiresAt, atPtr(time.Hour))
		checkTime(t, "created_at", got.CreatedAt, base)

		if err := s.DeleteWhitelist(ctx, e.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		_, err = s.WhitelistByID(ctx, e.ID)
		checkErr(t, "by id after delete", err, db.ErrNotFound)
		checkErr(t, "second delete", s.DeleteWhitelist(ctx, e.ID), db.ErrNotFound)
		_, err = s.WhitelistByID(ctx, NewID())
		checkErr(t, "unknown id", err, db.ErrNotFound)
	})
}

func whitelistIDs(list []model.WhitelistEntry) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.ID
	}
	return out
}

// Blocked IPs

func conformBlockedIPs(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()
	block := func(ip string, createdAt time.Time, expires *time.Time) *model.BlockedIP {
		return &model.BlockedIP{
			IP: ip, Reason: "autoblock", DenialCount: 3,
			FirstDeniedAt: createdAt.Add(-20 * time.Minute), LastDeniedAt: createdAt,
			CreatedAt: createdAt, ExpiresAt: expires,
		}
	}

	t.Run("UpsertInserts", func(t *testing.T) {
		s := open(t)
		b := mustBlock(t, s, block(ipA, base, nil))
		if b.ID == "" || b.IP != ipA || b.Reason != "autoblock" || b.DenialCount != 3 || b.HitCount != 0 {
			t.Fatalf("insert: got %+v", b)
		}
		checkTime(t, "first_denied_at", b.FirstDeniedAt, at(-20*time.Minute))
		checkTime(t, "last_denied_at", b.LastDeniedAt, base)
		checkTimePtr(t, "last_hit_at", b.LastHitAt, nil)
		checkTime(t, "created_at", b.CreatedAt, base)
		checkTimePtr(t, "expires_at", b.ExpiresAt, nil)

		noReason := block(ipB, base, atPtr(time.Hour))
		noReason.Reason = ""
		b2 := mustBlock(t, s, noReason)
		if b2.Reason != "autoblock" {
			t.Fatalf("empty reason stored as %q, want autoblock", b2.Reason)
		}
		checkTimePtr(t, "expires_at", b2.ExpiresAt, atPtr(time.Hour))
	})

	t.Run("ByIPFoundEvenWhenExpired", func(t *testing.T) {
		s := open(t)
		b := mustBlock(t, s, block(ipA, at(-2*time.Hour), atPtr(-time.Hour)))
		got, err := s.BlockedIPByIP(ctx, ipA)
		if err != nil {
			t.Fatalf("by ip: %v", err)
		}
		if got.ID != b.ID || got.Active(base) {
			t.Fatalf("by ip: got %+v, want the expired row %s", got, b.ID)
		}
		checkTimePtr(t, "expires_at", got.ExpiresAt, atPtr(-time.Hour))
		_, err = s.BlockedIPByIP(ctx, "192.0.2.1")
		checkErr(t, "unknown ip", err, db.ErrNotFound)
	})

	t.Run("Hit", func(t *testing.T) {
		s := open(t)
		b := mustBlock(t, s, block(ipA, base, nil))
		if err := s.RecordBlockedIPHit(ctx, b.ID, at(time.Minute)); err != nil {
			t.Fatalf("first hit: %v", err)
		}
		if err := s.RecordBlockedIPHit(ctx, b.ID, at(2*time.Minute)); err != nil {
			t.Fatalf("second hit: %v", err)
		}
		got, err := s.BlockedIPByIP(ctx, ipA)
		if err != nil {
			t.Fatalf("by ip: %v", err)
		}
		if got.HitCount != 2 {
			t.Fatalf("hit_count = %d, want 2", got.HitCount)
		}
		checkTimePtr(t, "last_hit_at", got.LastHitAt, atPtr(2*time.Minute))
		checkErr(t, "hit unknown id", s.RecordBlockedIPHit(ctx, NewID(), base), db.ErrNotFound)
	})

	t.Run("ListActiveNewestFirst", func(t *testing.T) {
		s := open(t)
		b1 := mustBlock(t, s, block(ipA, at(1*time.Minute), nil))
		b2 := mustBlock(t, s, block(ipB, at(2*time.Minute), atPtr(time.Hour)))
		mustBlock(t, s, block("192.0.2.1", at(3*time.Minute), atPtr(5*time.Minute)))  // expired at list time
		mustBlock(t, s, block("192.0.2.2", at(4*time.Minute), atPtr(10*time.Minute))) // expires exactly at list time

		list, err := s.ListBlockedIPs(ctx, at(10*time.Minute))
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		checkIDs(t, "list", blockedIDs(list), []string{b2.ID, b1.ID})
		checkTime(t, "created_at", list[0].CreatedAt, at(2*time.Minute))
	})

	t.Run("UpsertRearmsKeepingID", func(t *testing.T) {
		s := open(t)
		b1 := mustBlock(t, s, block(ipA, base, nil))
		if err := s.RecordBlockedIPHit(ctx, b1.ID, at(time.Minute)); err != nil {
			t.Fatalf("hit: %v", err)
		}
		if err := s.UnblockIP(ctx, b1.ID, at(time.Hour)); err != nil {
			t.Fatalf("unblock: %v", err)
		}

		b2 := mustBlock(t, s, &model.BlockedIP{
			IP: ipA, Reason: "manual", DenialCount: 5,
			FirstDeniedAt: at(90 * time.Minute), LastDeniedAt: at(2 * time.Hour),
			CreatedAt: at(2 * time.Hour), ExpiresAt: atPtr(3 * time.Hour),
		})
		if b2.ID != b1.ID {
			t.Fatalf("re-arm changed the id: %s, want %s", b2.ID, b1.ID)
		}
		check := func(b *model.BlockedIP) {
			t.Helper()
			if b.Reason != "manual" || b.DenialCount != 5 || b.HitCount != 0 {
				t.Fatalf("re-armed: got %+v", b)
			}
			checkTime(t, "first_denied_at", b.FirstDeniedAt, at(90*time.Minute))
			checkTime(t, "last_denied_at", b.LastDeniedAt, at(2*time.Hour))
			checkTimePtr(t, "last_hit_at", b.LastHitAt, nil)
			checkTime(t, "created_at", b.CreatedAt, at(2*time.Hour))
			checkTimePtr(t, "expires_at", b.ExpiresAt, atPtr(3*time.Hour))
		}
		check(b2)
		got, err := s.BlockedIPByIP(ctx, ipA)
		if err != nil {
			t.Fatalf("by ip: %v", err)
		}
		check(got)
		list, err := s.ListBlockedIPs(ctx, at(2*time.Hour))
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		checkIDs(t, "list", blockedIDs(list), []string{b1.ID})
	})

	t.Run("Unblock", func(t *testing.T) {
		s := open(t)
		b := mustBlock(t, s, block(ipA, base, nil))
		if err := s.UnblockIP(ctx, b.ID, at(time.Hour)); err != nil {
			t.Fatalf("unblock: %v", err)
		}
		got, err := s.BlockedIPByIP(ctx, ipA)
		if err != nil {
			t.Fatalf("by ip: %v", err)
		}
		checkTimePtr(t, "expires_at", got.ExpiresAt, atPtr(time.Hour))
		if !got.Active(at(30*time.Minute)) || got.Active(at(time.Hour)) {
			t.Fatalf("active flags wrong after unblock: %+v", got)
		}
		list, err := s.ListBlockedIPs(ctx, at(time.Hour))
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("list after unblock: %d rows, want 0", len(list))
		}
		checkErr(t, "second unblock", s.UnblockIP(ctx, b.ID, at(2*time.Hour)), db.ErrNotFound)
		checkErr(t, "unknown id", s.UnblockIP(ctx, NewID(), base), db.ErrNotFound)

		timed := mustBlock(t, s, block(ipB, base, atPtr(time.Hour)))
		checkErr(t, "unblock an expired block", s.UnblockIP(ctx, timed.ID, at(2*time.Hour)), db.ErrNotFound)
		if err := s.UnblockIP(ctx, timed.ID, at(30*time.Minute)); err != nil {
			t.Fatalf("unblock a timed block before its expiry: %v", err)
		}
	})
}

func blockedIDs(list []model.BlockedIP) []string {
	out := make([]string, len(list))
	for i, b := range list {
		out[i] = b.ID
	}
	return out
}

// Geo rules

func conformGeoRules(t *testing.T, open func(t *testing.T) db.Store) {
	ctx := context.Background()

	t.Run("Create", func(t *testing.T) {
		s := open(t)
		g, err := s.CreateGeoRule(ctx, "KP", str("no staff there"), base)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if g.ID == "" || g.Country != "KP" {
			t.Fatalf("create: got %+v", g)
		}
		checkStrPtr(t, "note", g.Note, str("no staff there"))
		checkTime(t, "created_at", g.CreatedAt, base)

		g2, err := s.CreateGeoRule(ctx, "FR", nil, base)
		if err != nil {
			t.Fatalf("create without note: %v", err)
		}
		checkStrPtr(t, "note", g2.Note, nil)
	})

	t.Run("Conflict", func(t *testing.T) {
		s := open(t)
		if _, err := s.CreateGeoRule(ctx, "KP", nil, base); err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err := s.CreateGeoRule(ctx, "KP", str("again"), base)
		checkErr(t, "duplicate", err, db.ErrConflict)
	})

	t.Run("ByCountry", func(t *testing.T) {
		s := open(t)
		g, err := s.CreateGeoRule(ctx, "KP", str("note"), base)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.GeoRuleByCountry(ctx, "KP")
		if err != nil {
			t.Fatalf("by country: %v", err)
		}
		if got.ID != g.ID || got.Country != "KP" {
			t.Fatalf("by country: got %+v", got)
		}
		checkStrPtr(t, "note", got.Note, str("note"))
		checkTime(t, "created_at", got.CreatedAt, base)
		_, err = s.GeoRuleByCountry(ctx, "FR")
		checkErr(t, "unknown country", err, db.ErrNotFound)
	})

	t.Run("ListOrderedByCountry", func(t *testing.T) {
		s := open(t)
		for _, c := range []string{"RU", "CN", "KP"} {
			if _, err := s.CreateGeoRule(ctx, c, nil, base); err != nil {
				t.Fatalf("create %s: %v", c, err)
			}
		}
		list, err := s.ListGeoRules(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		countries := make([]string, len(list))
		for i, g := range list {
			countries[i] = g.Country
		}
		checkIDs(t, "list", countries, []string{"CN", "KP", "RU"})
	})

	t.Run("Delete", func(t *testing.T) {
		s := open(t)
		g, err := s.CreateGeoRule(ctx, "KP", nil, base)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.DeleteGeoRule(ctx, g.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		_, err = s.GeoRuleByCountry(ctx, "KP")
		checkErr(t, "by country after delete", err, db.ErrNotFound)
		checkErr(t, "second delete", s.DeleteGeoRule(ctx, g.ID), db.ErrNotFound)
		checkErr(t, "unknown id", s.DeleteGeoRule(ctx, NewID()), db.ErrNotFound)
	})
}
