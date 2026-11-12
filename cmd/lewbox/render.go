package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/harlew-dev/lewbox/internal/service"
)

// renderAt prints v as text for people. Times are relative to now.
func renderAt(w io.Writer, v any, now time.Time) error {
	switch data := v.(type) {
	case service.NodeList:
		return renderNodes(w, data.Data)
	case *service.NodeList:
		return renderNodes(w, data.Data)
	case service.ClusterList:
		return renderClusters(w, data.Data)
	case *service.ClusterList:
		return renderClusters(w, data.Data)
	case service.UserList:
		return renderUsers(w, data.Data, now)
	case *service.UserList:
		return renderUsers(w, data.Data, now)
	case service.ClusterDetail:
		return renderClusterDetail(w, data, now)
	case *service.ClusterDetail:
		return renderClusterDetail(w, *data, now)
	case service.JoinToken:
		return renderJoinToken(w, data, now)
	case *service.JoinToken:
		return renderJoinToken(w, *data, now)
	case service.OrganizationList:
		return renderOrganizations(w, data.Data)
	case service.RegionList:
		return renderRegions(w, data.Data)
	case service.PlanList:
		return renderPlans(w, data.Data)
	case service.SubscriptionList:
		return renderSubscriptions(w, data.Data)
	case service.Subscription:
		return renderSubscription(w, data)
	case service.MemberList:
		return renderMembers(w, data.Data, now)
	case service.AdminKeyList:
		return renderAdminKeys(w, data.Data, now)
	case service.NewAdminKey:
		return renderNewAdminKey(w, data, now)
	case service.SandboxKeyList:
		return renderSandboxKeys(w, data.Data, now)
	case service.NewSandboxKey:
		return renderNewSandboxKey(w, data, now)
	case service.SetupLinkList:
		return renderSetupLinks(w, data.Data, now)
	case service.SandboxList:
		return renderSandboxes(w, data.Data, now)
	case service.SnapshotList:
		return renderSnapshots(w, data, now)
	case service.Usage:
		return renderUsage(w, data)
	case service.LimitsReport:
		return renderLimits(w, data)
	case service.ClusterHealth:
		return renderClusterHealth(w, data)
	case service.ClusterMetrics:
		return renderClusterMetrics(w, data)
	case service.Overview:
		return renderOverview(w, data)
	case contextList:
		return renderContexts(w, data.Data)
	case firewallOutput:
		return renderFirewall(w, data)
	case *firewallOutput:
		return renderFirewall(w, *data)
	default:
		return renderObject(w, v, now)
	}
}

// renderNodes prints a node table.
func renderNodes(w io.Writer, nodes []service.Node) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tCLUSTER\tROLE\tVERSION\tALIVE\tURL")
	for _, n := range nodes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			n.Name, dash(n.Cluster), dash(n.Role), dash(n.Version), aliveText(n.Alive), n.URL)
	}
	return tw.Flush()
}

// renderClusters prints a cluster table.
func renderClusters(w io.Writer, clusters []service.Cluster) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "SLUG\tRUNTIME\tSTATUS\tNODES\tVERSION\tENDPOINT")
	for _, c := range clusters {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			c.Slug, c.Runtime, c.Status, c.Nodes, dash(c.Version), c.EndpointURL)
	}
	return tw.Flush()
}

// renderUsers prints an account table.
func renderUsers(w io.Writer, users []service.User, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "EMAIL\tADMIN\tDISABLED\tCREATED")
	for _, u := range users {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			u.Email, yesNo(u.InstanceAdmin), yesNo(u.Disabled), relativeTime(u.CreatedAt, now))
	}
	return tw.Flush()
}

// renderClusterDetail prints the cluster fields, its checks and its nodes.
func renderClusterDetail(w io.Writer, d service.ClusterDetail, now time.Time) error {
	if err := renderObject(w, d.Cluster, now); err != nil {
		return err
	}
	if len(d.Checks) > 0 {
		fmt.Fprintln(w)
		tw := newTable(w)
		fmt.Fprintln(tw, "CHECK\tOK\tDETAIL")
		for _, c := range d.Checks {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, yesNo(c.OK), dash(c.Detail))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if len(d.NodeList) > 0 {
		fmt.Fprintln(w)
		return renderNodes(w, d.NodeList)
	}
	return nil
}

// renderJoinToken prints the command, which carries the token, and when it
// expires.
func renderJoinToken(w io.Writer, j service.JoinToken, now time.Time) error {
	fmt.Fprintf(w, "command: %s\n", j.Command)
	fmt.Fprintf(w, "expires: %s\n", relativeTime(j.ExpiresAt, now))
	return nil
}

// renderOrganizations prints an organization table. ROLE is the listed
// account's role, when it is a member.
func renderOrganizations(w io.Writer, orgs []service.Organization) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "SLUG\tNAME\tSTATUS\tREGION\tMEMBERS\tROLE")
	for _, o := range orgs {
		region := ""
		if o.Region != nil {
			region = o.Region.Slug
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n", o.Slug, o.Name, o.Status, dash(region), o.Members, dash(o.Role))
	}
	return tw.Flush()
}

// renderRegions prints the clusters customers can use.
func renderRegions(w io.Writer, regions []service.Region) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "SLUG\tNAME\tRUNTIME\tSTATUS\tENDPOINT")
	for _, r := range regions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Slug, r.Name, r.Runtime, r.Status, r.EndpointURL)
	}
	return tw.Flush()
}

// renderPlans prints a plan table. A limit of "-" is unlimited.
func renderPlans(w io.Writer, plans []service.Plan) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "SLUG\tNAME\tSANDBOXES\tVCPUS\tMEMORY\tSUBSCRIPTIONS\tDEFAULT")
	for _, p := range plans {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			p.Slug, p.Name,
			limitText(p.MaxSandboxes, ""), limitText(p.MaxVcpus, ""), limitText(p.MaxMemoryMB, "MB"),
			p.Subscriptions, yesNo(p.Default))
	}
	return tw.Flush()
}

// renderSubscriptions prints one organization's subscriptions by region.
func renderSubscriptions(w io.Writer, subs []service.Subscription) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "REGION\tNAME\tPLAN\tSANDBOXES\tVCPUS\tMEMORY\tCREATED")
	for _, s := range subs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Region.Slug, s.Region.Name, s.Plan.Slug,
			limitText(s.Limits.Sandboxes, ""), limitText(s.Limits.Vcpus, ""), limitText(s.Limits.MemoryMB, "MB"),
			relativeTime(s.CreatedAt, time.Now()))
	}
	return tw.Flush()
}

func renderSubscription(w io.Writer, s service.Subscription) error {
	return renderObject(w, s, time.Now())
}

func limitText(n *int64, unit string) string {
	if n == nil {
		return "-"
	}
	return fmt.Sprintf("%d%s", *n, unit)
}

// renderMembers prints a member table.
func renderMembers(w io.Writer, members []service.Member, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "EMAIL\tROLE\tADDED")
	for _, m := range members {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Email, m.Role, relativeTime(m.CreatedAt, now))
	}
	return tw.Flush()
}

// renderAdminKeys prints an admin key table; the key column is what is
// left of the token to recognise it by.
func renderAdminKeys(w io.Writer, keys []service.AdminKey, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tKEY\tSTATUS\tCREATED_BY\tLAST_USED\tEXPIRES")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s…%s\t%s\t%s\t%s\t%s\n",
			k.Name, k.Prefix, k.Last4, k.Status, dash(k.CreatedBy),
			optionalTime(k.LastUsedAt, now, "never"), optionalTime(k.ExpiresAt, now, "never"))
	}
	return tw.Flush()
}

// renderNewAdminKey prints a new key with its token, which can't be shown
// again.
func renderNewAdminKey(w io.Writer, k service.NewAdminKey, now time.Time) error {
	if err := renderObject(w, k.AdminKey, now); err != nil {
		return err
	}
	fmt.Fprintf(w, "\ntoken: %s\n", k.Token)
	fmt.Fprintln(w, "Save the token now: Lewbox keeps only its hash and cannot show it again.")
	return nil
}

func optionalTime(t *time.Time, now time.Time, none string) string {
	if t == nil {
		return none
	}
	return relativeTime(*t, now)
}

// renderSandboxKeys prints a sandbox key table; the key column is what is
// left of the token to recognise it by.
func renderSandboxKeys(w io.Writer, keys []service.SandboxKey, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tREGION\tKEY\tSTATUS\tCREATED_BY\tCREATED\tEXPIRES")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			k.Name, k.Region.Slug, k.TokenHint, sandboxKeyStatus(k), dash(k.CreatedBy),
			relativeTime(k.CreatedAt, now), optionalTime(k.ExpiresAt, now, "never"))
	}
	return tw.Flush()
}

func sandboxKeyStatus(k service.SandboxKey) string {
	switch {
	case k.RevokedAt != nil:
		return "revoked"
	case k.Expired:
		return "expired"
	default:
		return "active"
	}
}

// renderNewSandboxKey prints a new key with its token and the settings to
// paste into a sandbox client.
func renderNewSandboxKey(w io.Writer, k service.NewSandboxKey, now time.Time) error {
	if err := renderObject(w, k.SandboxKey, now); err != nil {
		return err
	}
	fmt.Fprintf(w, "\ntoken: %s\nSB_API_URL=%s\nSB_API_TOKEN=%s\n",
		k.Token, k.Env["SB_API_URL"], k.Env["SB_API_TOKEN"])
	if len(k.E2BEnv) > 0 {
		fmt.Fprintf(w, "E2B_API_URL=%s\nE2B_SANDBOX_URL=%s\nE2B_API_KEY=%s\n",
			k.E2BEnv["E2B_API_URL"], k.E2BEnv["E2B_SANDBOX_URL"], k.E2BEnv["E2B_API_KEY"])
	}
	fmt.Fprintln(w, "Save the token now: Lewbox keeps only its last four characters and cannot show it again.")
	return nil
}

// renderSetupLinks prints a setup link table; a token is never in it.
func renderSetupLinks(w io.Writer, links []service.SetupLink, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "ID\tPLAN\tREGION\tNOTE\tSTATUS\tCLAIMED_BY\tWORKSPACE\tEXPIRES")
	for _, l := range links {
		claimed, workspace := "-", "-"
		if l.ClaimedBy != nil {
			claimed = *l.ClaimedBy
		}
		if l.Workspace != nil {
			workspace = l.Workspace.Slug
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			l.ID, l.Plan.Slug, l.Region.Slug, dash(l.Note), l.Status, claimed, workspace,
			relativeTime(l.ExpiresAt, now))
	}
	return tw.Flush()
}

// renderSetupLinkCreated prints a new link with its token and the path the
// customer opens. The token can't be shown again.
func renderSetupLinkCreated(w io.Writer, l service.SetupLink, serverURL string, now time.Time) error {
	link := l
	link.Token = ""
	if err := renderObject(w, link, now); err != nil {
		return err
	}
	path := "/setup/" + l.Token
	fmt.Fprintf(w, "\ntoken: %s\n", l.Token)
	if serverURL != "" {
		fmt.Fprintf(w, "link: %s%s\n", strings.TrimRight(serverURL, "/"), path)
	} else {
		fmt.Fprintf(w, "link: %s\n", path)
	}
	fmt.Fprintln(w, "This link works once: Lewbox keeps only its hash and cannot show it again.")
	return nil
}

// renderSandboxes prints a sandbox table.
func renderSandboxes(w io.Writer, sandboxes []service.Sandbox, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "ID\tREGION\tSTATUS\tNAME\tIMAGE\tCPU\tMEMORY\tCREATED")
	for _, s := range sandboxes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%g\t%d\t%s\n",
			s.ID, s.Region, s.Status, dash(s.Name), dash(s.Image), s.CPU, s.MemoryMB, relativeTime(s.CreatedAt, now))
	}
	return tw.Flush()
}

// renderSnapshots prints a snapshot table and any nodes that did not answer.
func renderSnapshots(w io.Writer, list service.SnapshotList, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tREGION\tSOURCE\tCREATED\tCPU\tMEMORY\tDISK\tPUSH_STATE")
	for _, s := range list.Data {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%g\t%d\t%d\t%s\n",
			s.Name, s.Region, dash(s.SourceSandboxID), relativeTime(s.CreatedAt, now),
			s.CPU, s.MemoryMB, s.DiskGB, dash(s.PushState))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(list.PartialNodes) > 0 {
		fmt.Fprintf(w, "partial: %s did not answer\n", strings.Join(list.PartialNodes, ", "))
	}
	return nil
}

// renderUsage prints the totals and one row per bucket.
func renderUsage(w io.Writer, u service.Usage) error {
	fmt.Fprintf(w, "from: %s\n", u.From.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "to: %s\n", u.To.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "granularity: %s\n", u.Granularity)
	fmt.Fprintf(w, "totals: %.3f sandbox-hours, %.3f vCPU-hours, %.3f GiB-hours\n",
		u.Totals.SandboxHours, u.Totals.VcpuHours, u.Totals.MemoryGiBHours)

	tw := newTable(w)
	fmt.Fprintln(w, "START\tSANDBOX_H\tVCPU_H\tGIB_H\tPEAK")
	for _, b := range u.Data {
		fmt.Fprintf(tw, "%s\t%.3f\t%.3f\t%.3f\t%d\n",
			b.Start.UTC().Format(time.RFC3339), b.SandboxHours, b.VcpuHours, b.MemoryGiBHours, b.PeakSandboxes)
	}
	return tw.Flush()
}

// renderLimits prints each subscribed region's plan, limits, live use and
// pushed quota.
func renderLimits(w io.Writer, l service.LimitsReport) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "REGION\tPLAN\tLIMITS\tUSED\tQUOTA\tBLOCKED\tPUSHED\tERROR")
	for _, r := range l.Regions {
		quota := "none"
		if r.Quota != nil {
			quota = fmt.Sprintf("%d sandboxes, %g vCPUs, %d MB",
				r.Quota.MaxSandboxes, r.Quota.MaxCPU, r.Quota.MaxMemoryMB)
		}
		pushed := "never"
		if r.PushedAt != nil {
			pushed = relativeTime(*r.PushedAt, time.Now())
		}
		used := fmt.Sprintf("%d sandboxes, %g vCPUs, %d MB", r.Used.Sandboxes, r.Used.Vcpus, r.Used.MemoryMB)
		limits := fmt.Sprintf("%s sandboxes, %s vCPUs, %s MB",
			limitText(r.Limits.Sandboxes, ""), limitText(r.Limits.Vcpus, ""), limitText(r.Limits.MemoryMB, ""))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Slug, r.Plan.Slug, limits, used, quota, yesNo(r.Blocked), pushed, dash(r.Error))
	}
	return tw.Flush()
}

// renderContexts prints the saved contexts, marking the current one.
func renderContexts(w io.Writer, entries []contextEntry) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tAPI_URL\tCURRENT")
	for _, e := range entries {
		current := ""
		if e.Current {
			current = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, dash(e.APIURL), current)
	}
	return tw.Flush()
}

// renderFirewall prints the enabled state and the rules ufw now has.
func renderFirewall(w io.Writer, f firewallOutput) error {
	if f.DryRun {
		fmt.Fprintln(w, "dry run: nothing changed")
	} else {
		fmt.Fprintln(w, "ufw enabled")
	}
	fmt.Fprintf(w, "reset: %s\n", yesNo(f.Reset))

	tw := newTable(w)
	fmt.Fprintln(tw, "PORT\tPROTO\tFROM\tFOR")
	for _, rule := range f.Rules {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", rule.Port, rule.Proto, rule.From, rule.Purpose)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(f.Kept) > 0 {
		fmt.Fprintf(w, "kept: %s\n", strings.Join(f.Kept, ", "))
	}
	return nil
}

// renderObject prints any JSON object as aligned key: value lines. Nested
// values stay compact JSON and timestamps become relative.
func renderObject(w io.Writer, v any, now time.Time) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		_, err := fmt.Fprintln(w, compact(data))
		return err
	}
	keys := make([]string, 0, len(fields))
	width := 0
	for key := range fields {
		keys = append(keys, key)
		if len(key) > width {
			width = len(key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(w, "%-*s: %s\n", width, key, formatRaw(key, fields[key], now)); err != nil {
			return err
		}
	}
	return nil
}

// formatRaw formats one field of a JSON object.
func formatRaw(key string, raw json.RawMessage, now time.Time) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return "-"
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if strings.HasSuffix(key, "_at") {
				if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
					return relativeTime(t, now)
				}
			}
			if text == "" {
				return "-"
			}
			return text
		}
	}
	if strings.HasPrefix(value, "{") {
		// A nested account reads best as its email, e.g. "user: a@x.dev
		// (admin)" in whoami and status.
		var who struct {
			Email         string `json:"email"`
			InstanceAdmin bool   `json:"instance_admin"`
		}
		if json.Unmarshal(raw, &who) == nil && who.Email != "" {
			if who.InstanceAdmin {
				return who.Email + " (instance admin)"
			}
			return who.Email
		}
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return compact(raw)
	}
	return value
}

func compact(data []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return strings.TrimSpace(string(data))
	}
	return buf.String()
}

// relativeTime renders a past time as "5m ago". Future times keep their
// timestamp so they stay useful.
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func aliveText(alive *bool) string {
	if alive == nil {
		return "-"
	}
	return yesNo(*alive)
}

// renderClusterHealth prints one compact bar per cluster or node.
func renderClusterHealth(w io.Writer, h service.ClusterHealth) error {
	tw := newTable(w)
	fmt.Fprintf(tw, "NAME\tUPTIME\t%dD\n", len(h.Cluster.Days))
	fmt.Fprintf(tw, "cluster\t%s\t%s\n", percent(h.Cluster.Uptime), uptimeBar(h.Cluster.Days))
	for _, n := range h.Nodes {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", n.Name, percent(n.Uptime), uptimeBar(n.Days))
	}
	return tw.Flush()
}

// renderClusterMetrics prints one row per node bucket.
func renderClusterMetrics(w io.Writer, m service.ClusterMetrics) error {
	fmt.Fprintf(w, "range: %s, bucket: %s\n", m.Range, bucketText(m.BucketSeconds))
	tw := newTable(w)
	fmt.Fprintln(tw, "NODE\tSTART\tSANDBOXES\tVCPUS\tMEMORY_MB\tDISK_GB\tALIVE")
	for _, node := range m.Nodes {
		for _, point := range node.Data {
			fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%.0f\t%.0f\t%.2f\n",
				node.Name, point.Start.UTC().Format("2006-01-02 15:04"), point.Sandboxes,
				point.CpuReserved, point.MemoryUsedMB, point.DiskFreeGB, point.Alive)
		}
	}
	return tw.Flush()
}

// renderOverview prints the fleet counts, each cluster's bar and the busiest
// customers.
func renderOverview(w io.Writer, o service.Overview) error {
	fmt.Fprintf(w, "customers: %d (%d pending, %d suspended)\n", o.Customers.Total, o.Customers.Pending, o.Customers.Suspended)
	fmt.Fprintf(w, "sandboxes: %d running, %d stopped\n", o.Sandboxes.Running, o.Sandboxes.Stopped)
	fmt.Fprintf(w, "capacity: %.2f/%.0f vCPUs, %d/%d MB memory, %d/%d nodes alive\n",
		o.Capacity.CpuReserved, o.Capacity.CpuCores, o.Capacity.MemoryUsedMB, o.Capacity.MemoryTotalMB,
		o.Capacity.NodesAlive, o.Capacity.Nodes)

	tw := newTable(w)
	fmt.Fprintln(tw, "CLUSTER\tSTATUS\tUPTIME\t30D")
	for _, c := range o.Clusters {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Slug, c.Status, percent(c.Uptime), uptimeBar(c.Days))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(o.TopCustomers) > 0 {
		fmt.Fprintln(w, "\ntop customers this month:")
		for _, c := range o.TopCustomers {
			fmt.Fprintf(w, "  %s\t%.3f vCPU-hours\n", c.Name, c.VcpuHours)
		}
	}
	return nil
}

func percent(uptime *float64) string {
	if uptime == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *uptime*100)
}

func uptimeBar(days []service.UptimeDay) string {
	var b strings.Builder
	for _, day := range days {
		switch {
		case day.Uptime == nil:
			b.WriteString("·")
		case *day.Uptime >= 1:
			b.WriteString("█")
		default:
			b.WriteString("▒")
		}
	}
	return b.String()
}

func bucketText(seconds int64) string {
	if seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	return fmt.Sprintf("%dm", seconds/60)
}
