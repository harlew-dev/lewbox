package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	// defaultAuditRetention keeps the audit trail for 90 days. Zero keeps it
	// forever.
	defaultAuditRetention = 90 * 24 * time.Hour
	// eventPruneBatch bounds one delete; the prune repeats until a batch is
	// not full, so it never holds the database for long.
	eventPruneBatch = 1000
	// eventPageDefault and eventPageMax bound one page of the audit log.
	eventPageDefault = 50
	eventPageMax     = 200
)

// eventNameRE mirrors the CHECK on events.name: two or three snake_case
// segments, each starting with a letter.
var eventNameRE = regexp.MustCompile(`^[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}$`)

// Change is one field's before and after value in an event.
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Event is one row of the audit trail: who changed what, when and from where.
// Passwords, hashes, tokens and other secrets are never recorded.
type Event struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ActorKind      string            `json:"actor_kind"`
	ActorID        string            `json:"actor_id,omitempty"`
	ActorLabel     string            `json:"actor_label"`
	TargetType     string            `json:"target_type"`
	TargetID       string            `json:"target_id"`
	TargetName     string            `json:"target_name,omitempty"`
	OrganizationID string            `json:"organization_id,omitempty"`
	Changes        map[string]Change `json:"changes,omitempty"`
	IP             string            `json:"ip,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

// EventList is one cursor page of the audit log, newest first. Next is the
// cursor for the following page, or nil when this is the last one.
type EventList struct {
	Data []Event `json:"data"`
	Next *string `json:"next"`
}

// ListEventsRequest filters the audit log. Every field is optional.
type ListEventsRequest struct {
	Before       string `json:"before,omitempty"`       // the ID of the last row seen
	Actor        string `json:"actor,omitempty"`        // an account ID or email
	Organization string `json:"organization,omitempty"` // a workspace ID or slug
	Action       string `json:"action,omitempty"`       // an event name
	From         string `json:"from,omitempty"`         // RFC 3339, inclusive
	To           string `json:"to,omitempty"`           // RFC 3339, exclusive
	Limit        int    `json:"limit,omitempty"`
}

// queries returns the queries bound to the current transaction, when the
// context carries one, and the service's own queries otherwise.
func (s *Service) queries(ctx context.Context) *db.Queries {
	if q, ok := ctx.Value(queriesKey{}).(*db.Queries); ok {
		return q
	}
	return s.q
}

type queriesKey struct{}

// withTx runs fn in one transaction. Every query fn makes through the
// service's methods and helpers uses that transaction, so a change and its
// audit event commit together or not at all. A call already inside a
// transaction joins it.
func (s *Service) withTx(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(queriesKey{}).(*db.Queries); ok {
		return fn(ctx)
	}
	if s.database == nil {
		return errors.New("service has no database handle")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(context.WithValue(ctx, queriesKey{}, s.queries(ctx).WithTx(tx))); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type requestIDKey struct{}

// WithRequestID records the server's request ID for events written during a
// request.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type systemActorKey struct{}

// WithSystemActor marks a background pass: events it records are attributed to
// the system, not to an anonymous caller.
func WithSystemActor(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemActorKey{}, true)
}

func systemActorFrom(ctx context.Context) bool {
	ok, _ := ctx.Value(systemActorKey{}).(bool)
	return ok
}

// record writes one event. Inside withTx it joins the change's transaction;
// outside one it is written on its own, for facts with no data change such as
// a failed sign-in. The event is filled from the context: the caller, the
// client IP and the request ID.
func (s *Service) record(ctx context.Context, e Event) error {
	if !eventNameRE.MatchString(e.Name) {
		return fmt.Errorf("event name %q is not <resource>.<fact>", e.Name)
	}
	if e.ActorKind == "" {
		s.actor(ctx, &e)
	}
	if e.IP == "" {
		e.IP = clientIPFrom(ctx)
	}
	if e.RequestID == "" {
		e.RequestID = requestIDFrom(ctx)
	}
	if e.ID == "" {
		e.ID = uuid.NewV7().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.now()
	}
	var changes sql.NullString
	if len(e.Changes) > 0 {
		encoded, err := json.Marshal(e.Changes)
		if err != nil {
			return fmt.Errorf("encode event changes: %w", err)
		}
		changes = sql.NullString{String: string(encoded), Valid: true}
	}
	params := db.InsertEventParams{
		ID:         e.ID,
		Name:       e.Name,
		ActorKind:  e.ActorKind,
		ActorLabel: e.ActorLabel,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		TargetName: e.TargetName,
		Changes:    changes,
		Ip:         sql.NullString{String: e.IP, Valid: e.IP != ""},
		RequestID:  sql.NullString{String: e.RequestID, Valid: e.RequestID != ""},
		CreatedAt:  e.CreatedAt,
	}
	if e.ActorID != "" {
		params.ActorID = sql.NullString{String: e.ActorID, Valid: true}
	}
	if e.OrganizationID != "" {
		params.OrganizationID = sql.NullString{String: e.OrganizationID, Valid: true}
	}
	if err := s.queries(ctx).InsertEvent(ctx, params); err != nil {
		return fmt.Errorf("record event %s: %w", e.Name, err)
	}
	return nil
}

// actor fills who acted from the context's principal.
func (s *Service) actor(ctx context.Context, e *Event) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		if systemActorFrom(ctx) {
			e.ActorKind, e.ActorLabel = "system", "system"
		} else {
			e.ActorKind = "anonymous"
		}
		return
	}
	e.ActorID = p.UserID
	e.ActorLabel = p.Email
	switch p.Kind {
	case "user":
		if p.TokenID != "" {
			e.ActorKind = "access_token"
			return
		}
		e.ActorKind = "user"
	case "key":
		e.ActorKind = "admin_key"
		e.ActorID = p.KeyID
		e.ActorLabel = p.KeyName
	case "local":
		e.ActorKind, e.ActorID, e.ActorLabel = "local", "", "local"
	default:
		e.ActorKind = p.Kind
	}
}

// ListEvents returns one cursor page of the audit trail, newest first.
// Instance admins only.
func (s *Service) ListEvents(ctx context.Context, req ListEventsRequest) (EventList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return EventList{}, err
	}
	out := EventList{Data: []Event{}}

	action := strings.TrimSpace(req.Action)
	if action != "" && !eventNameRE.MatchString(action) {
		return EventList{}, invalidField("action", "action must be an event name such as plan.updated")
	}
	from, err := parseEventTime(req.From, "from")
	if err != nil {
		return EventList{}, err
	}
	to, err := parseEventTime(req.To, "to")
	if err != nil {
		return EventList{}, err
	}
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		return EventList{}, invalidField("to", "to must be after from")
	}
	limit := req.Limit
	switch {
	case limit == 0:
		limit = eventPageDefault
	case limit < 1 || limit > eventPageMax:
		return EventList{}, invalidField("limit", "limit must be between 1 and %d", eventPageMax)
	}

	actorID := ""
	if ref := strings.TrimSpace(req.Actor); ref != "" {
		actorID, err = s.eventActorFilter(ctx, ref)
		if err != nil {
			return EventList{}, err
		}
		if actorID == "" {
			return out, nil
		}
	}
	orgID := ""
	if ref := strings.TrimSpace(req.Organization); ref != "" {
		orgID, err = s.eventOrganizationFilter(ctx, ref)
		if err != nil {
			return EventList{}, err
		}
		if orgID == "" {
			return out, nil
		}
	}

	params := db.ListEventsParams{
		ActorID:        actorID,
		OrganizationID: orgID,
		Name:           action,
		BeforeID:       "",
		RowLimit:       int64(limit + 1),
	}
	if !from.IsZero() {
		params.FromTime = from
	}
	if !to.IsZero() {
		params.ToTime = to
	}
	if before := strings.TrimSpace(req.Before); before != "" {
		cursor, err := s.queries(ctx).GetEventByID(ctx, before)
		if errors.Is(err, sql.ErrNoRows) {
			return EventList{}, invalid("that page is no longer available")
		}
		if err != nil {
			return EventList{}, fmt.Errorf("get event cursor: %w", err)
		}
		if !eventMatchesCursor(cursor, actorID, orgID, action, from, to) {
			return EventList{}, invalid("that page is no longer available")
		}
		params.BeforeID = cursor.ID
		params.BeforeTime = cursor.CreatedAt
	}

	rows, err := s.queries(ctx).ListEvents(ctx, params)
	if err != nil {
		return EventList{}, fmt.Errorf("list events: %w", err)
	}
	next := false
	if len(rows) > limit {
		rows = rows[:limit]
		next = true
	}
	out.Data = make([]Event, 0, len(rows))
	for _, row := range rows {
		out.Data = append(out.Data, toEvent(row))
	}
	if next && len(out.Data) > 0 {
		id := out.Data[len(out.Data)-1].ID
		out.Next = &id
	}
	return out, nil
}

// eventActorFilter resolves an account ID or email to an account ID. An
// unknown account matches nothing.
func (s *Service) eventActorFilter(ctx context.Context, ref string) (string, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id.String(), nil
	}
	row, err := s.queries(ctx).GetUserByEmail(ctx, strings.ToLower(ref))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get user for event filter: %w", err)
	}
	return row.ID, nil
}

// eventOrganizationFilter resolves a workspace ID or slug to a workspace ID.
// An unknown workspace matches nothing.
func (s *Service) eventOrganizationFilter(ctx context.Context, ref string) (string, error) {
	if id, err := uuid.Parse(ref); err == nil {
		row, err := s.queries(ctx).GetOrganizationByID(ctx, id.String())
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("get organization for event filter: %w", err)
		}
		return row.ID, nil
	}
	row, err := s.queries(ctx).GetOrganizationBySlug(ctx, strings.ToLower(ref))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get organization for event filter: %w", err)
	}
	return row.ID, nil
}

// eventMatchesCursor reports whether the cursor row belongs to the page's
// scope, so a cursor from another filter is refused instead of skipping rows.
func eventMatchesCursor(row db.Event, actorID, orgID, action string, from, to time.Time) bool {
	if actorID != "" && (!row.ActorID.Valid || row.ActorID.String != actorID) {
		return false
	}
	if orgID != "" && (!row.OrganizationID.Valid || row.OrganizationID.String != orgID) {
		return false
	}
	if action != "" && row.Name != action {
		return false
	}
	if !from.IsZero() && row.CreatedAt.Before(from) {
		return false
	}
	if !to.IsZero() && !row.CreatedAt.Before(to) {
		return false
	}
	return true
}

func parseEventTime(raw, field string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, invalidField(field, "%s must be an RFC 3339 time, such as 2026-11-24T00:00:00Z", field)
	}
	return t.UTC(), nil
}

// nullStringValue turns an optional ID into a JSON-friendly value.
func nullStringValue(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

// nullIntValue turns a nullable limit into a JSON-friendly value.
func nullIntValue(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

// optString turns an optional string into a JSON-friendly value.
func optString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func toEvent(row db.Event) Event {
	e := Event{
		ID:         row.ID,
		Name:       row.Name,
		ActorKind:  row.ActorKind,
		ActorLabel: row.ActorLabel,
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		TargetName: row.TargetName,
		CreatedAt:  row.CreatedAt,
	}
	if row.ActorID.Valid {
		e.ActorID = row.ActorID.String
	}
	if row.OrganizationID.Valid {
		e.OrganizationID = row.OrganizationID.String
	}
	if row.Ip.Valid {
		e.IP = row.Ip.String
	}
	if row.RequestID.Valid {
		e.RequestID = row.RequestID.String
	}
	if row.Changes.Valid && row.Changes.String != "" {
		_ = json.Unmarshal([]byte(row.Changes.String), &e.Changes)
	}
	return e
}

// pruneEvents deletes events past the retention. A zero retention keeps them.
func (s *Service) pruneEvents(ctx context.Context, now time.Time) error {
	if s.auditRetention <= 0 {
		return nil
	}
	cutoff := now.Add(-s.auditRetention)
	for {
		n, err := s.queries(ctx).DeleteEventsBefore(ctx, db.DeleteEventsBeforeParams{
			CreatedAt: cutoff,
			Limit:     eventPruneBatch,
		})
		if err != nil {
			return fmt.Errorf("prune events: %w", err)
		}
		if n < eventPruneBatch {
			return nil
		}
	}
}
