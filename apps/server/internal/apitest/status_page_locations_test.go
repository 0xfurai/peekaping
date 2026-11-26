package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

type showLocationsPage struct {
	Slug          string `json:"slug"`
	ShowLocations bool   `json:"show_locations"`
}

func TestStatusPagesAPI_ShowLocationsDefaultsToOffAndRoundTrips(t *testing.T) {
	a := newAppWithAdmin(t)
	path := "/api/v1/status-pages"

	plain := a.createStatusPage(t, statusPage{slug: "plain"})
	got := decode[showLocationsPage](t, a.request(t, http.MethodGet, path+"/"+plain, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, showLocationsPage{Slug: "plain", ShowLocations: false}, got, "a page made without the setting")

	made := decode[struct {
		ID            string `json:"id"`
		ShowLocations bool   `json:"show_locations"`
	}](t, a.request(t, http.MethodPost, path, map[string]any{"slug": "eachplace", "title": "Each place", "show_locations": true}, a.asAdmin), http.StatusCreated)
	assert.True(t, made.ShowLocations, "the response to the create")
	got = decode[showLocationsPage](t, a.request(t, http.MethodGet, path+"/"+made.ID, nil, a.asAdmin), http.StatusOK)
	assert.True(t, got.ShowLocations, "and what is stored")

	updated := decode[showLocationsPage](t, a.request(t, http.MethodPatch, path+"/"+made.ID, map[string]any{"title": "Renamed"}, a.asAdmin), http.StatusOK)
	assert.True(t, updated.ShowLocations, "an update that leaves the setting out keeps it")

	updated = decode[showLocationsPage](t, a.request(t, http.MethodPatch, path+"/"+made.ID, map[string]any{"show_locations": false}, a.asAdmin), http.StatusOK)
	assert.False(t, updated.ShowLocations, "an update turns it off")
	updated = decode[showLocationsPage](t, a.request(t, http.MethodPatch, path+"/"+plain, map[string]any{"show_locations": true}, a.asAdmin), http.StatusOK)
	assert.True(t, updated.ShowLocations, "and on")
	got = decode[showLocationsPage](t, a.request(t, http.MethodGet, path+"/"+plain, nil, a.asAdmin), http.StatusOK)
	assert.True(t, got.ShowLocations)

	list := decode[[]showLocationsPage](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	bySlug := map[string]bool{}
	for _, p := range list {
		bySlug[p.Slug] = p.ShowLocations
	}
	assert.Equal(t, map[string]bool{"plain": true, "eachplace": false}, bySlug, "the list")
}
