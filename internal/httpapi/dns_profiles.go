package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// DNSProfiles are saved Cloudflare credentials. {dns_profile} is an ID or
// name.
type DNSProfiles struct{ *server }

// Index lists every DNS profile.
func (h DNSProfiles) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListDNSProfiles(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Store saves and tests a DNS profile.
func (h DNSProfiles) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateDNSProfileRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateDNSProfile(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one DNS profile.
func (h DNSProfiles) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetDNSProfile(r.Context(), r.PathValue("dns_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Update changes a DNS profile.
func (h DNSProfiles) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateDNSProfileRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdateDNSProfile(r.Context(), r.PathValue("dns_profile"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a DNS profile no cluster uses.
func (h DNSProfiles) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeleteDNSProfile(r.Context(), r.PathValue("dns_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Check tests a DNS profile's token now.
func (h DNSProfiles) Check(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.TestDNSProfile(r.Context(), r.PathValue("dns_profile"))
	h.respond(w, http.StatusOK, resp, err)
}

// Credentials returns a DNS profile's token, for a node.
func (h DNSProfiles) Credentials(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DNSProfileCredentials(r.Context(), r.PathValue("dns_profile"))
	h.respond(w, http.StatusOK, resp, err)
}
