package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

var geoRulesContractKeys = []string{"id", "country", "note", "created_at"}

func geoRulesCreate(e *env, country string, note *string) model.GeoRuleItem {
	e.t.Helper()
	st, body := e.admin(http.MethodPost, "/geo-rules", model.GeoRuleBody{Country: country, Note: note})
	mustStatus(e.t, st, http.StatusCreated, body)
	return decode[model.GeoRuleItem](e.t, body)
}

func geoRulesList(e *env) []model.GeoRuleItem {
	e.t.Helper()
	st, body := e.admin(http.MethodGet, "/geo-rules", nil)
	mustStatus(e.t, st, http.StatusOK, body)
	return decode[model.ListResponse[model.GeoRuleItem]](e.t, body).Items
}

func TestGeoRulesCreate(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/geo-rules", []byte(`{"country": "kp", "note": "no staff there"}`))
	mustStatus(t, st, http.StatusCreated, body)
	devicesMustHaveExactKeys(t, decodeMap(t, body), geoRulesContractKeys...)

	rule := decode[model.GeoRuleItem](t, body)
	if !isUUID(rule.ID) || rule.Country != "KP" || rule.Note == nil || *rule.Note != "no staff there" || !rule.CreatedAt.Equal(baseTime) {
		t.Fatalf("body %s", body)
	}
}

func TestGeoRulesCreateNoteAbsentIsNull(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/geo-rules", []byte(`{"country": "FR"}`))
	mustStatus(t, st, http.StatusCreated, body)
	if v, ok := decodeMap(t, body)["note"]; !ok || v != nil {
		t.Fatalf("note: want present and null, body %s", body)
	}

	// A blank note is the same as no note.
	st, body = e.admin(http.MethodPost, "/geo-rules", []byte(`{"country": "DE", "note": "   "}`))
	mustStatus(t, st, http.StatusCreated, body)
	if v := decodeMap(t, body)["note"]; v != nil {
		t.Fatalf("blank note: want null, body %s", body)
	}
}

func TestGeoRulesCreateDuplicateIs409(t *testing.T) {
	e := newEnv(t)
	geoRulesCreate(e, "KP", strp("no staff there"))

	// Same country in another case and with spaces: still the same rule.
	st, body := e.admin(http.MethodPost, "/geo-rules", model.GeoRuleBody{Country: " kp "})
	mustStatus(t, st, http.StatusConflict, body)
	if got := decode[model.ErrorResponse](t, body).Error; got != "country already has a rule" {
		t.Fatalf("error %q", got)
	}
	if rules := geoRulesList(e); len(rules) != 1 {
		t.Fatalf("%d rules, want 1", len(rules))
	}
}

func TestGeoRulesCreateUnknownCountryIs400(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"XX", "FRA", "", "F", "   "} {
		st, body := e.admin(http.MethodPost, "/geo-rules", model.GeoRuleBody{Country: c})
		mustStatus(t, st, http.StatusBadRequest, body)
		if got := decode[model.ErrorResponse](t, body).Error; got != "unknown country code" {
			t.Fatalf("country %q: error %q", c, got)
		}
	}
	if rules := geoRulesList(e); len(rules) != 0 {
		t.Fatalf("rules created by rejected calls: %+v", rules)
	}
}

func TestGeoRulesCreateInvalidJSONIs400(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/geo-rules", []byte(`{"country": "KP",`))
	mustStatus(t, st, http.StatusBadRequest, body)
	if decode[model.ErrorResponse](t, body).Error == "" {
		t.Fatalf("body %s", body)
	}
}

func TestGeoRulesListOrderedByCountry(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"US", "kp", "Fr"} {
		geoRulesCreate(e, c, strp("note for "+c))
	}

	st, body := e.admin(http.MethodGet, "/geo-rules", nil)
	mustStatus(t, st, http.StatusOK, body)
	rules := decode[model.ListResponse[model.GeoRuleItem]](t, body).Items
	got := make([]string, 0, len(rules))
	for _, r := range rules {
		got = append(got, r.Country)
	}
	if strings.Join(got, ",") != "FR,KP,US" {
		t.Fatalf("order %v, want FR,KP,US", got)
	}
	for _, it := range decodeMap(t, body)["items"].([]any) {
		devicesMustHaveExactKeys(t, it.(map[string]any), geoRulesContractKeys...)
	}
}

func TestGeoRulesListEmptyIsNotNull(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/geo-rules", nil)
	mustStatus(t, st, http.StatusOK, body)
	items, ok := decodeMap(t, body)["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("want items: [], got %s", body)
	}
}

func TestGeoRulesDelete(t *testing.T) {
	e := newEnv(t)
	keep := geoRulesCreate(e, "KP", nil)
	gone := geoRulesCreate(e, "RU", nil)

	st, body := e.admin(http.MethodDelete, "/geo-rules/"+gone.ID, nil)
	mustStatus(t, st, http.StatusNoContent, body)
	if len(body) != 0 {
		t.Fatalf("204 with a body: %s", body)
	}

	st, body = e.admin(http.MethodDelete, "/geo-rules/"+gone.ID, nil)
	mustStatus(t, st, http.StatusNotFound, body)

	rules := geoRulesList(e)
	if len(rules) != 1 || rules[0].ID != keep.ID {
		t.Fatalf("after delete: %+v", rules)
	}

	// The country can be blocked again once its rule is gone.
	if again := geoRulesCreate(e, "RU", nil); again.ID == gone.ID {
		t.Fatalf("new rule reused the deleted id %s", gone.ID)
	}
}

func TestGeoRulesDeleteUnknownIs404(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{dbtest.NewID(), "not-a-uuid", "KP"} {
		st, body := e.admin(http.MethodDelete, "/geo-rules/"+id, nil)
		mustStatus(t, st, http.StatusNotFound, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("body %s", body)
		}
	}
}

func TestGeoRulesServerTokenIs403(t *testing.T) {
	e := newEnv(t)
	rule := geoRulesCreate(e, "KP", nil)
	calls := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/geo-rules", model.GeoRuleBody{Country: "RU"}},
		{http.MethodGet, "/geo-rules", nil},
		{http.MethodDelete, "/geo-rules/" + rule.ID, nil},
	}
	for _, c := range calls {
		st, body := e.agent(c.method, c.path, c.body)
		mustStatus(t, st, http.StatusForbidden, body)
	}
	if rules := geoRulesList(e); len(rules) != 1 {
		t.Fatalf("rules changed by a forbidden call: %+v", rules)
	}
}
