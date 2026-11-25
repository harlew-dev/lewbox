package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/service"
)

// addListFlags adds the paging, search and sort flags every paged list
// command shares. sortHelp names the list's sort keys.
func addListFlags(cmd *cobra.Command, q *service.ListQuery, sortHelp string) {
	cmd.Flags().StringVar(&q.Q, "search", "", "search this list")
	if sortHelp == "" {
		sortHelp = "a sort key; prefix it with - to reverse the order"
	}
	cmd.Flags().StringVar(&q.Sort, "sort", "", sortHelp)
	cmd.Flags().StringVar(&q.Page, "page", "", "the page to show (default 1)")
	cmd.Flags().StringVar(&q.PerPage, "per-page", "", "rows per page (default 20, at most 100)")
}

// pageLine formats the text-mode page summary for one paged list.
func pageLine(v any) (string, bool) {
	switch data := v.(type) {
	case service.OrganizationList:
		return formatPage(data.Pages, "workspaces")
	case service.UserList:
		return formatPage(data.Pages, "accounts")
	case service.SandboxList:
		return formatPage(data.Pages, "sandboxes")
	case service.SnapshotList:
		return formatPage(data.Pages, "snapshots")
	case service.NodeList:
		return formatPage(data.Pages, "nodes")
	case service.MemberList:
		return formatPage(data.Pages, "members")
	case service.SandboxKeyList:
		return formatPage(data.Pages, "sandbox keys")
	case service.AdminKeyList:
		return formatPage(data.Pages, "admin keys")
	case service.AccessTokenList:
		return formatPage(data.Pages, "access tokens")
	case service.SetupLinkList:
		return formatPage(data.Pages, "setup links")
	}
	return "", false
}

// formatPage is "Page 2 of 7 · 131 sandboxes".
func formatPage(p service.Pages, noun string) (string, bool) {
	if p.PerPage == 0 {
		return "", false
	}
	return fmt.Sprintf("Page %d of %d · %d %s", p.Current, p.Total, p.Items, noun), true
}
