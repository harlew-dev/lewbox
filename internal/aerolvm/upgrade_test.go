package aerolvm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
)

func TestUpgradeClient(t *testing.T) {
	ctx := context.Background()
	fake := newFake(t)
	fake.AdminToken = adminToken
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Role: "mixed", Alive: true, Version: "v0.7.0", Voter: true},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Role: "mixed", Alive: true, Version: "v0.7.0", Voter: true},
	})
	client := aerolvm.New(fake.URL(), adminToken)

	if _, err := client.UpgradeStatus(ctx); !errors.Is(err, aerolvm.ErrNoRollout) {
		t.Fatalf("status before rollout = %v", err)
	}

	req := aerolvm.UpgradeApplyRequest{
		Version: "v0.7.1",
		Files:   map[string]string{"sandboxd_linux_amd64": sum("sandboxd")},
		Source:  aerolvm.UpgradeSourceRelease,
	}
	_, err := client.StartUpgrade(ctx, req)
	var apiErr *aerolvm.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != aerolvm.UpgradeCodeNeedsDowntime || apiErr.Hint == "" {
		t.Fatalf("start without allow-downtime = %v", err)
	}

	req.AllowDowntime = true
	out, err := client.StartUpgrade(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.State != aerolvm.UpgradeStateRunning || out.TargetVersion != "v0.7.1" || len(out.Nodes) != 2 {
		t.Fatalf("start = %+v", out)
	}

	status, err := client.UpgradeStatus(ctx)
	if err != nil || status.ID != out.ID {
		t.Fatalf("status = %+v, %v", status, err)
	}

	for _, tc := range []struct {
		call func() (aerolvm.UpgradeRollout, error)
		want string
	}{
		{func() (aerolvm.UpgradeRollout, error) { return client.StopUpgrade(ctx) }, aerolvm.UpgradeStateStopped},
		{func() (aerolvm.UpgradeRollout, error) { return client.ResumeUpgrade(ctx) }, aerolvm.UpgradeStateRunning},
		{func() (aerolvm.UpgradeRollout, error) { return client.RollbackUpgrade(ctx) }, aerolvm.UpgradeStateRolledBack},
		{func() (aerolvm.UpgradeRollout, error) { return client.FinalizeUpgrade(ctx) }, aerolvm.UpgradeStateFinalized},
	} {
		got, err := tc.call()
		if err != nil || got.State != tc.want {
			t.Fatalf("command = %+v, %v; want %s", got, err, tc.want)
		}
	}
}

func TestUpgradeStatusTreatsMissingRouteAsNoRollout(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.AdminToken = adminToken
	fake.SetMembers([]aerolvm.Member{{NodeID: "node-a", APIURL: fake.URL(), Alive: true, Version: "v0.7.0"}})
	client := aerolvm.New(fake.URL(), adminToken)
	if _, err := client.UpgradeStatus(context.Background()); !errors.Is(err, aerolvm.ErrNoRollout) {
		t.Fatalf("err = %v", err)
	}
}
