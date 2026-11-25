package main

import (
	"testing"

	"github.com/harlew-dev/lewbox/internal/service"
)

func TestFormatPage(t *testing.T) {
	got, ok := formatPage(service.Pages{Current: 2, Total: 7, PerPage: 20, Items: 131}, "sandboxes")
	if !ok || got != "Page 2 of 7 · 131 sandboxes" {
		t.Errorf("page line = %q, %v", got, ok)
	}
}

func TestPageLineOnlyForPagedLists(t *testing.T) {
	if _, ok := pageLine(service.SubscriptionList{}); ok {
		t.Error("a subscription list got a page line")
	}
	if _, ok := pageLine(service.EventList{}); ok {
		t.Error("an event list got a page line")
	}
	if line, ok := pageLine(service.SandboxList{Pages: service.Pages{Current: 1, Total: 1, PerPage: 20, Items: 0}}); !ok || line != "Page 1 of 1 · 0 sandboxes" {
		t.Errorf("empty paged list = %q, %v", line, ok)
	}
}
