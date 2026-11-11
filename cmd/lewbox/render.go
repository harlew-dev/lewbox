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
	case service.MemberList:
		return renderMembers(w, data.Data, now)
	case service.AdminKeyList:
		return renderAdminKeys(w, data.Data, now)
	case service.NewAdminKey:
		return renderNewAdminKey(w, data, now)
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
	fmt.Fprintln(tw, "SLUG\tNAME\tSTATUS\tPLAN\tMEMBERS\tROLE")
	for _, o := range orgs {
		plan := ""
		if o.Plan != nil {
			plan = o.Plan.Slug
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n", o.Slug, o.Name, o.Status, dash(plan), o.Members, dash(o.Role))
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
	fmt.Fprintln(tw, "SLUG\tNAME\tSANDBOXES\tVCPUS\tMEMORY\tORGS\tDEFAULT")
	for _, p := range plans {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			p.Slug, p.Name,
			limitText(p.MaxSandboxes, ""), limitText(p.MaxVcpus, ""), limitText(p.MaxMemoryMB, "MB"),
			p.Organizations, yesNo(p.Default))
	}
	return tw.Flush()
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
