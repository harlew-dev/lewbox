package service

import (
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

func advancedPtr[T any](v T) *T { return &v }

func TestAdvancedSettingsCreateAndUpdate(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")

	req := clusterRequest(fake, "lab")
	req.DockerTimeout = "1h"
	req.MaxSandboxCPU = advancedPtr(4.0)
	req.MaxSandboxMemoryMB = advancedPtr(int64(4096))
	req.MaxSandboxDiskGB = advancedPtr(int64(50))
	req.IdleStopMinutes = advancedPtr(int64(30))
	req.DestroyIfIdleFor = "24h"
	req.MaxAge = "168h"
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DockerTimeout != "1h" || detail.MaxSandboxCPU == nil || *detail.MaxSandboxCPU != 4 ||
		detail.MaxSandboxMemoryMB == nil || *detail.MaxSandboxMemoryMB != 4096 ||
		detail.MaxSandboxDiskGB == nil || *detail.MaxSandboxDiskGB != 50 ||
		detail.IdleStopMinutes == nil || *detail.IdleStopMinutes != 30 ||
		detail.DestroyIfIdleFor != "24h" || detail.MaxAge != "168h" {
		t.Errorf("created advanced settings = %+v", detail.AdvancedSettings)
	}
	settings, err := s.ClusterNodeSettings(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if settings.DockerTimeout != "1h" || settings.MaxSandboxCPU == nil || *settings.MaxSandboxCPU != 4 ||
		settings.MaxSandboxMemoryMB == nil || *settings.MaxSandboxMemoryMB != 4096 ||
		settings.MaxSandboxDiskGB == nil || *settings.MaxSandboxDiskGB != 50 ||
		settings.IdleStopMinutes == nil || *settings.IdleStopMinutes != 30 ||
		settings.DestroyIfIdleFor != "24h" || settings.MaxAge != "168h" {
		t.Errorf("node settings = %+v", settings.AdvancedSettings)
	}

	// A PATCH changes only what it names.
	eight := 8.0
	updated, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{
		MaxSandboxCPU: OptionalOf(eight),
		MaxAge:        OptionalOf("720h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.MaxSandboxCPU == nil || *updated.MaxSandboxCPU != 8 || updated.MaxAge != "720h" ||
		updated.MaxSandboxMemoryMB == nil || *updated.MaxSandboxMemoryMB != 4096 {
		t.Errorf("updated advanced settings = %+v", updated.AdvancedSettings)
	}

	// null clears each one; docker_timeout returns to 15m.
	cleared, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{
		DockerTimeout:      OptionalNull[string](),
		MaxSandboxCPU:      OptionalNull[float64](),
		MaxSandboxMemoryMB: OptionalNull[int64](),
		MaxSandboxDiskGB:   OptionalNull[int64](),
		IdleStopMinutes:    OptionalNull[int64](),
		DestroyIfIdleFor:   OptionalNull[string](),
		MaxAge:             OptionalNull[string](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.DockerTimeout != "15m" || cleared.MaxSandboxCPU != nil || cleared.MaxSandboxMemoryMB != nil ||
		cleared.MaxSandboxDiskGB != nil || cleared.IdleStopMinutes != nil ||
		cleared.DestroyIfIdleFor != "" || cleared.MaxAge != "" {
		t.Errorf("cleared advanced settings = %+v", cleared.AdvancedSettings)
	}

	// The update is in the audit trail with from/to values.
	events, err := s.ListEvents(localCtx(), ListEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events.Data {
		if event.Name != "cluster.updated" || event.TargetName != "lab" {
			continue
		}
		if change, ok := event.Changes["max_sandbox_cpu"]; ok && change.From == "8" && change.To == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("no cluster.updated event records max_sandbox_cpu: %+v", events.Data)
	}
}

func TestAdvancedSettingsValidation(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")

	cases := []struct {
		name   string
		change func(*AddClusterRequest)
		want   string
	}{
		{"docker timeout too short", func(r *AddClusterRequest) { r.DockerTimeout = "30s" }, "docker_timeout must be between 1m and 2h"},
		{"docker timeout too long", func(r *AddClusterRequest) { r.DockerTimeout = "3h" }, "docker_timeout must be between 1m and 2h"},
		{"cpu below the default", func(r *AddClusterRequest) { r.MaxSandboxCPU = advancedPtr(0.5) }, "max_sandbox_cpu must be at least 1"},
		{"cpu zero", func(r *AddClusterRequest) { r.MaxSandboxCPU = advancedPtr(0.0) }, "max_sandbox_cpu must be at least 1"},
		{"memory below the default", func(r *AddClusterRequest) { r.MaxSandboxMemoryMB = advancedPtr(int64(512)) }, "max_sandbox_memory_mb must be at least 1024"},
		{"memory zero", func(r *AddClusterRequest) { r.MaxSandboxMemoryMB = advancedPtr(int64(0)) }, "max_sandbox_memory_mb must be at least 1024"},
		{"disk below the default", func(r *AddClusterRequest) { r.MaxSandboxDiskGB = advancedPtr(int64(5)) }, "max_sandbox_disk_gb must be at least 10"},
		{"disk zero", func(r *AddClusterRequest) { r.MaxSandboxDiskGB = advancedPtr(int64(0)) }, "max_sandbox_disk_gb must be at least 10"},
		{"negative idle stop", func(r *AddClusterRequest) { r.IdleStopMinutes = advancedPtr(int64(-1)) }, "idle_stop_minutes must be a whole number"},
		{"destroy idle too long", func(r *AddClusterRequest) { r.DestroyIfIdleFor = "721h" }, "destroy_if_idle_for must be at most 720h"},
		{"age too long", func(r *AddClusterRequest) { r.MaxAge = "721h" }, "max_age must be at most 720h"},
		{"bad duration", func(r *AddClusterRequest) { r.MaxAge = "soon" }, "max_age must be a duration"},
		{"slots on docker", func(r *AddClusterRequest) { r.FirecrackerVMSlots = advancedPtr(int64(512)) }, "firecracker_vm_slots only applies to Firecracker clusters"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := clusterRequest(fake, "bad-"+string(rune('a'+i)))
			tc.change(&req)
			_, err := s.AddCluster(localCtx(), req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if se, ok := err.(*Error); !ok || se.Code != CodeInvalid {
				t.Errorf("error = %#v, want a 422", err)
			}
		})
	}

	// The field is named in details.
	req := clusterRequest(fake, "bad-field")
	req.MaxSandboxCPU = advancedPtr(0.5)
	_, err := s.AddCluster(localCtx(), req)
	if se, ok := err.(*Error); !ok || se.Details != (FieldDetails{Field: "max_sandbox_cpu"}) {
		t.Errorf("details = %#v", err)
	}
}

func TestFirecrackerSlotsRaiseOnly(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	fake.SetMembers([]aerolvm.Member{testMember(fake, "node-fc", "firecracker", true)})
	req := clusterRequest(fake, "fc")
	req.Runtime = "firecracker"
	req.FirecrackerVMSlots = advancedPtr(int64(512))
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	if detail.FirecrackerVMSlots == nil || *detail.FirecrackerVMSlots != 512 {
		t.Errorf("slots = %+v", detail.FirecrackerVMSlots)
	}

	if _, err := s.UpdateCluster(localCtx(), "fc", UpdateClusterRequest{
		FirecrackerVMSlots: OptionalOf(int64(256)),
	}); err == nil || !strings.Contains(err.Error(), "cannot be lowered from 512") {
		t.Errorf("lowering: %v", err)
	}
	if _, err := s.UpdateCluster(localCtx(), "fc", UpdateClusterRequest{
		FirecrackerVMSlots: OptionalNull[int64](),
	}); err == nil || !strings.Contains(err.Error(), "cannot be cleared") {
		t.Errorf("clearing: %v", err)
	}
	updated, err := s.UpdateCluster(localCtx(), "fc", UpdateClusterRequest{
		FirecrackerVMSlots: OptionalOf(int64(1024)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.FirecrackerVMSlots == nil || *updated.FirecrackerVMSlots != 1024 {
		t.Errorf("raised slots = %+v", updated.FirecrackerVMSlots)
	}
	if _, err := s.UpdateCluster(localCtx(), "fc", UpdateClusterRequest{
		FirecrackerVMSlots: OptionalOf(int64(128)),
	}); err == nil || !strings.Contains(err.Error(), "between 256 and 1024") {
		t.Errorf("out of range: %v", err)
	}

	// A Firecracker cluster may start at AerolVM's default and stay there.
	plain := clusterRequest(fake, "fc-default")
	plain.Runtime = "firecracker"
	created, err := s.AddCluster(localCtx(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if created.FirecrackerVMSlots != nil {
		t.Errorf("default slots = %+v", created.FirecrackerVMSlots)
	}
	if _, err := s.UpdateCluster(localCtx(), "fc-default", UpdateClusterRequest{
		FirecrackerVMSlots: OptionalOf(int64(256)),
	}); err != nil {
		t.Errorf("setting the default explicitly: %v", err)
	}
}

func TestAdvancedSettingsDrift(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	req := clusterRequest(fake, "lab")
	req.MaxSandboxCPU = advancedPtr(4.0)
	req.IdleStopMinutes = advancedPtr(int64(30))
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	reportPolicy(t, s, detail.ID, detail.Runtime)

	node, err := s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"max_sandbox_cpu is 4; this node applied none",
		"idle_stop_minutes is 30; this node applied none",
	} {
		if !containsString(node.Settings.Reasons, want) {
			t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
		}
	}

	applied := DefaultAdvancedSettings()
	applied.MaxSandboxCPU = advancedPtr(4.0)
	applied.IdleStopMinutes = advancedPtr(int64(30))
	if _, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		PolicyRuntime: advancedPtr("docker"), Advanced: &applied,
	}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("matching advanced settings are outdated: %v", node.Settings.Reasons)
	}

	eight := 8.0
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{MaxSandboxCPU: OptionalOf(eight)}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "max_sandbox_cpu is 8; this node applied 4"
	if !containsString(node.Settings.Reasons, want) {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	checked, err := s.CheckCluster(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	check, ok := findCheck(checked.Checks, "node settings")
	if !ok || check.OK || !strings.Contains(check.Detail, "node-a: "+want) {
		t.Errorf("node settings check = %+v, found %v", check, ok)
	}
}

func TestAdvancedReportValidation(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail, err := s.AddCluster(localCtx(), clusterRequest(fake, "lab"))
	if err != nil {
		t.Fatal(err)
	}
	reportPolicy(t, s, detail.ID, detail.Runtime)

	bad := DefaultAdvancedSettings()
	bad.DockerTimeout = "3h"
	if _, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{Advanced: &bad}); err == nil ||
		!strings.Contains(err.Error(), "docker_timeout must be between 1m and 2h") {
		t.Errorf("bad report: %v", err)
	}

	// A Docker cluster cannot report a Firecracker pool.
	bad = DefaultAdvancedSettings()
	bad.FirecrackerVMSlots = advancedPtr(int64(512))
	if _, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{Advanced: &bad}); err == nil ||
		!strings.Contains(err.Error(), "only applies to Firecracker") {
		t.Errorf("slots on docker report: %v", err)
	}
}
