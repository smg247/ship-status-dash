package types

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"ship-status-dash/pkg/utils"
)

type Severity string

const (
	SeverityDown     Severity = "Down"
	SeverityDegraded Severity = "Degraded"
	// SeveritySuspected is used by the community reporting feature (non-admin outage reports).
	SeveritySuspected Severity = "Suspected"
	// SeverityCapacityExhausted is for components that can go into outage due to lack of resources. For example, a boskos cloud-account.
	SeverityCapacityExhausted Severity = "CapacityExhausted"
)

func (s Severity) ToStatus() Status {
	switch s {
	case SeverityDown:
		return StatusDown
	case SeverityDegraded:
		return StatusDegraded
	case SeverityCapacityExhausted:
		return StatusCapacityExhausted
	case SeveritySuspected:
		return StatusSuspected
	default:
		return Status("Invalid")
	}
}

// IsValidSeverity checks if the provided severity string is a valid severity level
func IsValidSeverity(severity string) bool {
	switch Severity(severity) {
	case SeverityDown, SeverityDegraded, SeveritySuspected, SeverityCapacityExhausted:
		return true
	default:
		return false
	}
}

// GetSeverityLevel returns a numeric value for severity comparison (higher = more critical)
func GetSeverityLevel(severity Severity) int {
	switch severity {
	case SeverityDown:
		return 4
	case SeverityDegraded:
		return 3
	case SeverityCapacityExhausted:
		return 2
	case SeveritySuspected:
		return 1
	default:
		return 0
	}
}

// CheckType represents the type of monitoring check to perform.
type CheckType string

const (
	CheckTypePrometheus CheckType = "prometheus"
	CheckTypeHTTP       CheckType = "http"
	CheckTypeSystemd    CheckType = "systemd"
	CheckTypeJUnit      CheckType = "junit"
	CheckTypeJira       CheckType = "jira"
)

// Outage represents a component outage with tracking information for incident management.
type Outage struct {
	gorm.Model
	ComponentName    string       `json:"component_name" gorm:"column:component_name;not null;index"`
	SubComponentName string       `json:"sub_component_name" gorm:"column:sub_component_name;not null;index"`
	Severity         Severity     `json:"severity" gorm:"column:severity;not null"`
	StartTime        time.Time    `json:"start_time" gorm:"column:start_time;not null;index"`
	EndTime          sql.NullTime `json:"end_time" gorm:"column:end_time;index"`
	Description      string       `json:"description" gorm:"column:description;type:text;not null"`
	// DiscoveredFrom describes where this outage was created: frontend, component-monitor, MCP, API
	DiscoveredFrom string       `json:"discovered_from" gorm:"column:discovered_from;not null"`
	CreatedBy      string       `json:"created_by" gorm:"column:created_by;not null"`
	ConfirmedAt    sql.NullTime `json:"confirmed_at" gorm:"column:confirmed_at"`
	// LastAuditableUpdate mirrors CreatedAt of the newest audit log for this outage.
	// Maintained by a Postgres trigger on outage_audit_logs inserts.
	LastAuditableUpdate time.Time `json:"last_auditable_update" gorm:"column:last_auditable_update;index"`
	// Reasons are the Reason records that describe the reason for the outage
	// this is utilized only by the component-monitor
	Reasons []Reason `json:"reasons,omitempty" gorm:"foreignKey:OutageID"`
	// SlackThreads are the Slack threads associated with the outage
	SlackThreads  []SlackThread        `json:"slack_threads,omitempty" gorm:"foreignKey:OutageID"`
	AuditLogs     []OutageAuditLog     `json:"audit_logs,omitempty" gorm:"foreignKey:OutageID"`
	Reports       []OutageReport       `json:"reports,omitempty" gorm:"foreignKey:OutageID"`
	TriageNotes   []TriageNote         `json:"triage_notes,omitempty" gorm:"foreignKey:OutageID"`
	Links         []OutageLink         `json:"links,omitempty" gorm:"foreignKey:OutageID"`
	Relationships []OutageRelationship `json:"relationships,omitempty" gorm:"foreignKey:OutageID"`
}

// Validate validates the outage and returns an error message and whether it's valid.
// Returns an empty string and true if valid, otherwise returns an aggregated error message and false.
func (o *Outage) Validate() (string, bool) {
	var validationErrors []string

	if o.Severity == "" {
		validationErrors = append(validationErrors, "Severity is required")
	} else if !IsValidSeverity(string(o.Severity)) {
		validationErrors = append(validationErrors, "Invalid severity. Must be one of: Down, Degraded, Suspected, CapacityExhausted")
	}

	if o.StartTime.IsZero() {
		validationErrors = append(validationErrors, "StartTime is required")
	}

	if strings.TrimSpace(o.Description) == "" {
		validationErrors = append(validationErrors, "Description is required")
	}

	if o.DiscoveredFrom == "" {
		validationErrors = append(validationErrors, "DiscoveredFrom is required")
	}

	if o.CreatedBy == "" {
		validationErrors = append(validationErrors, "CreatedBy is required")
	}

	if len(validationErrors) > 0 {
		return strings.Join(validationErrors, "; "), false
	}

	return "", true
}

type contextKey string

const (
	OldOutageKey   contextKey = "old_outage"
	CurrentUserKey contextKey = "current_user"
)

// normalizeOutageTimesUTC converts outage timestamps to UTC so audit log diffs
// do not show spurious timezone changes (pgx returns times in the session TZ).
func normalizeOutageTimesUTC(o *Outage) {
	o.StartTime = o.StartTime.UTC()
	if o.EndTime.Valid {
		o.EndTime.Time = o.EndTime.Time.UTC()
	}
	if o.ConfirmedAt.Valid {
		o.ConfirmedAt.Time = o.ConfirmedAt.Time.UTC()
	}
	if !o.LastAuditableUpdate.IsZero() {
		o.LastAuditableUpdate = o.LastAuditableUpdate.UTC()
	}
}

func (o *Outage) BeforeUpdate(db *gorm.DB) error {
	return o.before(db)
}

func (o *Outage) BeforeDelete(db *gorm.DB) error {
	return o.before(db)
}

func (o *Outage) before(db *gorm.DB) error {
	// Check if we've already captured the old outage in this transaction
	if existing := db.Statement.Context.Value(OldOutageKey); existing != nil {
		return nil
	}

	var old Outage
	if err := db.Preload("Reasons").Preload("SlackThreads").Preload("TriageNotes").Preload("Links").Preload("Relationships").First(&old, o.ID).Error; err != nil {
		return err
	}

	normalizeOutageTimesUTC(&old)

	db.Statement.Context = context.WithValue(db.Statement.Context, OldOutageKey, old)
	return nil
}

func (o *Outage) AfterUpdate(db *gorm.DB) error {
	return o.after(db, Update)
}

func (o *Outage) AfterCreate(db *gorm.DB) error {
	return o.after(db, Create)
}

func (o *Outage) AfterDelete(db *gorm.DB) error {
	return o.after(db, Delete)
}

func (o *Outage) after(db *gorm.DB, operation OperationType) error {
	var oldOutageJSON []byte
	if operation == Update || operation == Delete {
		var err error
		oldOutage, ok := db.Statement.Context.Value(OldOutageKey).(Outage)
		if !ok {
			return fmt.Errorf("value of old_outage is not an Outage type")
		}
		oldOutageJSON, err = json.Marshal(oldOutage)
		if err != nil {
			return fmt.Errorf("error marshaling old outage record: %w", err)
		}
	}

	var newTriageJSON []byte
	if operation != Delete {
		var fresh Outage
		if err := db.Preload("Reasons").Preload("SlackThreads").Preload("TriageNotes").Preload("Links").Preload("Relationships").First(&fresh, o.ID).Error; err != nil {
			return fmt.Errorf("failed to reload outage for audit: %w", err)
		}
		normalizeOutageTimesUTC(&fresh)
		var err error
		newTriageJSON, err = json.Marshal(fresh)
		if err != nil {
			return fmt.Errorf("error marshaling new outage record: %w", err)
		}
	}
	userVal := db.Statement.Context.Value(CurrentUserKey)
	if userVal == nil {
		return fmt.Errorf("current user not found in context")
	}
	userStr, ok := userVal.(string)
	if !ok {
		return fmt.Errorf("current user in context has invalid type %T, expected string", userVal)
	}
	audit := OutageAuditLog{
		Operation: string(operation),
		OutageID:  o.ID,
		User:      userStr,
		Old:       oldOutageJSON,
		New:       newTriageJSON,
	}

	return db.Create(&audit).Error
}

// ReportedLink is a URL carried on a component-monitor report reason.
type ReportedLink struct {
	URL      string   `json:"url"`
	LinkType LinkType `json:"link_type,omitempty"`
}

// Normalize validates and normalizes a reported link for storage on an outage.
// It returns the trimmed URL, resolved link type, and whether the link is valid.
func (l ReportedLink) Normalize() (string, LinkType, bool) {
	raw := strings.TrimSpace(l.URL)
	if raw == "" {
		return "", "", false
	}
	parsed, ok := utils.ParseHTTPURL(raw)
	if !ok || parsed.Host == "" {
		return "", "", false
	}
	linkType := l.LinkType
	if linkType == "" {
		linkType = LinkTypeOther
	} else if !IsValidLinkType(string(linkType)) {
		return "", "", false
	}
	return raw, linkType, true
}

type Reason struct {
	gorm.Model
	OutageID uint `json:"-" gorm:"column:outage_id;not null;index"`
	// Type defines the type of monitoring check that was performed.
	// Valid values are defined by CheckType: prometheus, http, systemd, junit, or jira.
	Type CheckType `json:"type"`
	// Check defines the specific check that was performed:
	// prometheus query, HTTP URL, systemd unit name, junit Prow job name, or Jira issue key.
	Check string `json:"check"`
	// Results summarizes the results of the check
	Results string `json:"results"`
	// Links are accepted on component-monitor reports and copied onto the outage
	// as outage_links. They are not stored on the reasons table.
	Links []ReportedLink `json:"links,omitempty" gorm:"-"`
}

// ComponentReportPing represents a ping report from a component monitor.
// This is used to track the last time that any status has been reported for a component/sub-component.
type ComponentReportPing struct {
	gorm.Model
	ComponentName    string    `json:"component_name" gorm:"column:component_name;not null;index;uniqueIndex:idx_component_subcomponent"`
	SubComponentName string    `json:"sub_component_name" gorm:"column:sub_component_name;not null;index;uniqueIndex:idx_component_subcomponent"`
	Time             time.Time `json:"time" gorm:"column:time;not null;index"`
}

// SlackThread represents a Slack thread associated with an outage in a specific channel.
type SlackThread struct {
	gorm.Model
	OutageID        uint   `json:"outage_id" gorm:"column:outage_id;not null;index:idx_outage_channel,unique"`
	Channel         string `json:"channel" gorm:"column:channel;not null;index:idx_outage_channel,unique"`
	ChannelID       string `json:"channel_id" gorm:"column:channel_id;not null"`
	ThreadTimestamp string `json:"thread_timestamp" gorm:"column:thread_timestamp;not null"`
	ThreadURL       string `json:"thread_url" gorm:"column:thread_url;not null"`
}

type OperationType string

const (
	Create OperationType = "CREATE"
	Update OperationType = "UPDATE"
	Delete OperationType = "DELETE"
)

type OutageAuditLog struct {
	gorm.Model
	OutageID  uint   `json:"outage_id" gorm:"column:outage_id;not null;index"`
	User      string `json:"user" gorm:"column:user;not null"`
	Operation string `json:"operation" gorm:"column:operation;not null"`
	Old       []byte `json:"old,omitempty" gorm:"column:old;type:jsonb"`
	New       []byte `json:"new,omitempty" gorm:"column:new;type:jsonb"`
}

// OutageReport tracks an individual user's report of a suspected outage.
type OutageReport struct {
	gorm.Model
	OutageID uint   `json:"outage_id" gorm:"column:outage_id;not null;uniqueIndex:idx_outage_report_user"`
	User     string `json:"user" gorm:"column:user;not null;uniqueIndex:idx_outage_report_user"`
}

// TriageNote represents a single note added to an outage during triage.
type TriageNote struct {
	gorm.Model
	OutageID uint   `json:"outage_id" gorm:"column:outage_id;not null;index"`
	Body     string `json:"body" gorm:"column:body;type:text;not null"`
	Author   string `json:"author" gorm:"column:author;not null"`
}

// LinkType represents the category of an outage link.
type LinkType string

const (
	LinkTypeIncidentChannelThread LinkType = "incident_channel_thread"
	LinkTypeRCA                   LinkType = "rca"
	LinkTypeJira                  LinkType = "jira"
	LinkTypeOther                 LinkType = "other"
)

func IsValidLinkType(lt string) bool {
	switch LinkType(lt) {
	case LinkTypeIncidentChannelThread, LinkTypeRCA, LinkTypeJira, LinkTypeOther:
		return true
	default:
		return false
	}
}

// OutageLink represents a user-curated URL associated with an outage (e.g. Jira, runbook, incident channel).
type OutageLink struct {
	gorm.Model
	OutageID    uint     `json:"outage_id" gorm:"column:outage_id;not null;index"`
	URL         string   `json:"url" gorm:"column:url;not null"`
	LinkType    LinkType `json:"link_type" gorm:"column:link_type;not null;default:'other'"`
	Description string   `json:"description" gorm:"column:description;type:text"`
}

// RelationshipType represents the type of relationship between two outages.
type RelationshipType string

const (
	RelationshipCauses    RelationshipType = "causes"
	RelationshipCausedBy  RelationshipType = "caused_by"
	RelationshipRelatedTo RelationshipType = "related_to"
)

func IsValidRelationshipType(rt string) bool {
	switch RelationshipType(rt) {
	case RelationshipCauses, RelationshipCausedBy, RelationshipRelatedTo:
		return true
	default:
		return false
	}
}

// InverseRelationshipType returns the inverse of a directional relationship type.
func InverseRelationshipType(rt RelationshipType) RelationshipType {
	switch rt {
	case RelationshipCauses:
		return RelationshipCausedBy
	case RelationshipCausedBy:
		return RelationshipCauses
	default:
		return RelationshipRelatedTo
	}
}

// SLOWorkspaceItem is one persisted SLO workspace row. details is jsonb whose shape
// is defined by kind and schema_version.
type SLOWorkspaceItem struct {
	gorm.Model
	Team          string             `json:"team" gorm:"column:team;not null;uniqueIndex:idx_slo_item_identity"`
	Kind          string             `json:"kind" gorm:"column:kind;not null;uniqueIndex:idx_slo_item_identity"`
	SchemaVersion int                `json:"schema_version" gorm:"column:schema_version;not null"`
	ItemKey       string             `json:"item_key" gorm:"column:item_key;not null;uniqueIndex:idx_slo_item_identity"`
	GroupKey      string             `json:"group_key" gorm:"column:group_key;index"`
	OccurredAt    time.Time          `json:"occurred_at" gorm:"column:occurred_at;not null;index"`
	Outcome       string             `json:"outcome" gorm:"column:outcome;not null"`
	Details       []byte             `json:"details" gorm:"column:details;type:jsonb"`
	Notes         string             `json:"notes" gorm:"column:notes;type:text"`
	UpdatedBy     string             `json:"updated_by" gorm:"column:updated_by"`
	Links         []SLOWorkspaceLink `json:"links,omitempty" gorm:"foreignKey:ItemID"`
}

// SLOWorkspaceLink is a Jira, outage, or other URL attached to a workspace item.
type SLOWorkspaceLink struct {
	gorm.Model
	ItemID   uint   `json:"item_id" gorm:"column:item_id;not null;index"`
	URL      string `json:"url" gorm:"column:url;not null"`
	LinkType string `json:"link_type" gorm:"column:link_type;not null"`
	OutageID *uint  `json:"outage_id,omitempty" gorm:"column:outage_id"`
}

// OutageRelationship represents a first-class relationship between two outages.
// Reciprocal rows are always stored: if A causes B, a row for B caused_by A also exists.
type OutageRelationship struct {
	gorm.Model
	OutageID         uint             `json:"outage_id" gorm:"column:outage_id;not null;uniqueIndex:idx_outage_relationship"`
	RelatedOutageID  uint             `json:"related_outage_id" gorm:"column:related_outage_id;not null;uniqueIndex:idx_outage_relationship"`
	RelationshipType RelationshipType `json:"relationship_type" gorm:"column:relationship_type;not null"`
	RelatedOutage    *Outage          `json:"related_outage,omitempty" gorm:"foreignKey:RelatedOutageID"`
}
