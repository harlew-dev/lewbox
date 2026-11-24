package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// StorageProfiles are saved S3-compatible bucket credentials.
// {storage_profile} is an ID or name.
type StorageProfiles struct{ *server }

// Index lists every storage profile.
func (h StorageProfiles) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListStorageProfiles(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Store saves and tests a storage profile.
func (h StorageProfiles) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateStorageProfileRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateStorageProfile(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one storage profile.
func (h StorageProfiles) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetStorageProfile(r.Context(), r.PathValue("storage_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Update changes a storage profile.
func (h StorageProfiles) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateStorageProfileRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdateStorageProfile(r.Context(), r.PathValue("storage_profile"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a storage profile no cluster uses.
func (h StorageProfiles) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeleteStorageProfile(r.Context(), r.PathValue("storage_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Check tests a storage profile's bucket and key now.
func (h StorageProfiles) Check(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.TestStorageProfile(r.Context(), r.PathValue("storage_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Credentials returns a storage profile's secret key, for a node.
func (h StorageProfiles) Credentials(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.StorageProfileCredentials(r.Context(), r.PathValue("storage_profile"))
	h.respond(w, http.StatusOK, resp, err)
}
