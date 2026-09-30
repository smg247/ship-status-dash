package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/sets"

	"ship-status-dash/pkg/auth"
	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
	"ship-status-dash/pkg/utils"
)

// Handlers contains the HTTP request handlers for the dashboard API.
type Handlers struct {
	logger                 *logrus.Logger
	configManager          *config.Manager[types.DashboardConfig]
	outageManager          outage.OutageManager
	pingRepo               repositories.ComponentPingRepository
	triageNoteRepo         repositories.TriageNoteRepository
	outageLinkRepo         repositories.OutageLinkRepository
	sloRepo                repositories.SLOWorkspaceRepository
	groupCache             auth.GroupMembershipProvider
	monitorReportProcessor *ComponentMonitorReportProcessor
	externalPageCaches     map[string]*ExternalPageCache
}

// NewHandlers creates a new Handlers instance with the provided dependencies.
func NewHandlers(logger *logrus.Logger, configManager *config.Manager[types.DashboardConfig], outageManager outage.OutageManager, pingRepo repositories.ComponentPingRepository, triageNoteRepo repositories.TriageNoteRepository, outageLinkRepo repositories.OutageLinkRepository, sloRepo repositories.SLOWorkspaceRepository, groupCache auth.GroupMembershipProvider) *Handlers {
	return &Handlers{
		logger:                 logger,
		configManager:          configManager,
		outageManager:          outageManager,
		pingRepo:               pingRepo,
		triageNoteRepo:         triageNoteRepo,
		outageLinkRepo:         outageLinkRepo,
		sloRepo:                sloRepo,
		groupCache:             groupCache,
		monitorReportProcessor: NewComponentMonitorReportProcessor(outageManager, pingRepo, configManager, logger),
		externalPageCaches: map[string]*ExternalPageCache{
			"spc-dashboard": NewExternalPageCache(
				"https://storage.googleapis.com/ship-spc-dashboard/index.html",
				1*time.Hour,
				logger,
			),
		},
	}
}

// config returns the current dashboard configuration.
func (h *Handlers) config() *types.DashboardConfig {
	return h.configManager.Get()
}

func respondWithJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data) // Best effort - can't return error after writing headers
}

func respondWithError(w http.ResponseWriter, statusCode int, message string) {
	respondWithJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

// collectAuthorizedIdentities returns all identities authorized for a component:
// Owner.User, Owner.ServiceAccount values, and expanded RoverGroup members.
func (h *Handlers) collectAuthorizedIdentities(component *types.Component) []string {
	return h.collectOwnerIdentities(component.Owners)
}

func (h *Handlers) collectOwnerIdentities(owners []types.Owner) []string {
	identities := sets.NewString()
	for _, owner := range owners {
		if owner.User != "" {
			identities.Insert(owner.User)
		}
		if owner.ServiceAccount != "" {
			identities.Insert(owner.ServiceAccount)
		}
		if owner.RoverGroup != "" {
			identities.Insert(h.groupCache.GetGroupMembers(owner.RoverGroup)...)
		}
	}
	return identities.UnsortedList()
}

// IsUserAuthorizedForComponent checks if a user is authorized to perform mutating actions on a component.
func (h *Handlers) IsUserAuthorizedForComponent(user string, component *types.Component) bool {
	return slices.Contains(h.collectAuthorizedIdentities(component), user)
}

// HealthJSON returns the health status of the dashboard service.
func (h *Handlers) HealthJSON(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}
	respondWithJSON(w, http.StatusOK, response)
}

// GetComponentsJSON returns the list of configured components.
func (h *Handlers) GetComponentsJSON(w http.ResponseWriter, r *http.Request) {
	components := h.config().Components
	visible := make([]*types.Component, 0, len(components))
	for _, component := range components {
		if component.SLOComponent {
			continue
		}
		visible = append(visible, component)
	}
	respondWithJSON(w, http.StatusOK, visible)
}

// GetComponentInfoJSON returns the information for a specific component.
func (h *Handlers) GetComponentInfoJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}
	respondWithJSON(w, http.StatusOK, component)
}

// GetOutagesJSON retrieves outages for a specific component, aggregating sub-component outages for top-level components.
func (h *Handlers) GetOutagesJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]

	logger := h.logger.WithField("component", componentName)

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}
	subComponentSlugs := make([]string, len(component.Subcomponents))
	for i, subComponent := range component.Subcomponents {
		subComponentSlugs[i] = subComponent.Slug
	}

	outages, err := h.outageManager.GetOutagesForComponent(componentName, subComponentSlugs)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query outages from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outages")
		return
	}

	respondWithJSON(w, http.StatusOK, outages)
}

// GetSubComponentOutagesJSON retrieves outages for a specific sub-component.
func (h *Handlers) GetSubComponentOutagesJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-component not found")
		return
	}

	outages, err := h.outageManager.GetOutagesForSubComponent(componentName, subComponentName)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query outages from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outages")
		return
	}

	respondWithJSON(w, http.StatusOK, outages)
}

// CreateOutageJSON creates a new outage for a sub-component.
func (h *Handlers) CreateOutageJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}
	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	var outageReq types.UpsertOutageRequest
	if err := json.NewDecoder(r.Body).Decode(&outageReq); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to create outage")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	severity := ""
	if outageReq.Severity != nil {
		severity = *outageReq.Severity
	}
	discoveredFrom := ""
	if outageReq.DiscoveredFrom != nil {
		discoveredFrom = *outageReq.DiscoveredFrom
	}
	logger = logger.WithFields(logrus.Fields{
		"severity":        severity,
		"discovered_from": discoveredFrom,
	})

	var description string
	if outageReq.Description != nil {
		description = strings.TrimSpace(*outageReq.Description)
	}

	outage := types.Outage{
		ComponentName:    componentName,
		SubComponentName: subComponentName,
		Severity:         types.Severity(severity),
		Description:      description,
		StartTime:        *outageReq.StartTime,
		DiscoveredFrom:   discoveredFrom,
	}

	outage.CreatedBy = activeUser

	if outageReq.EndTime != nil {
		outage.EndTime = *outageReq.EndTime
	}

	confirmed := (outageReq.Confirmed != nil && *outageReq.Confirmed)
	if confirmed || !subComponent.RequiresConfirmation {
		outage.ConfirmedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	if message, valid := outage.Validate(); !valid {
		respondWithError(w, http.StatusBadRequest, message)
		return
	}

	var initialTriageNote string
	if outageReq.InitialTriageNote != nil {
		initialTriageNote = strings.TrimSpace(*outageReq.InitialTriageNote)
	}

	if err := h.outageManager.CreateOutage(&outage, nil, activeUser, initialTriageNote); err != nil {
		logger.WithField("error", err).Error("Failed to create outage in database")
		respondWithError(w, http.StatusInternalServerError, "Failed to create outage")
		return
	}

	logger.Infof("Successfully created outage: %d", outage.ID)

	respondWithJSON(w, http.StatusCreated, outage)
}

// UpdateOutageJSON updates an existing outage with the provided fields.
func (h *Handlers) UpdateOutageJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"outage_id":     outageID,
		"component":     componentName,
		"sub_component": subComponentName,
		"active_user":   activeUser,
	})
	logger.Info("Updating outage")

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	var updateReq types.UpsertOutageRequest
	if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to update outage")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	if updateReq.Severity != nil {
		if !types.IsValidSeverity(*updateReq.Severity) {
			respondWithError(w, http.StatusBadRequest, "Invalid severity. Must be one of: Down, Degraded, Suspected")
			return
		}
		outage.Severity = types.Severity(*updateReq.Severity)
	}
	if updateReq.StartTime != nil && !updateReq.StartTime.Equal(outage.StartTime) {
		outage.StartTime = *updateReq.StartTime
	}
	if updateReq.EndTime != nil {
		endTimeChanged := updateReq.EndTime.Valid != outage.EndTime.Valid || !updateReq.EndTime.Time.Equal(outage.EndTime.Time)
		if endTimeChanged {
			outage.EndTime = *updateReq.EndTime
		}
	}
	if updateReq.Description != nil {
		outage.Description = strings.TrimSpace(*updateReq.Description)
	}
	if updateReq.Confirmed != nil {
		if *updateReq.Confirmed && !outage.ConfirmedAt.Valid {
			outage.ConfirmedAt = sql.NullTime{Time: time.Now(), Valid: true}
		} else if !*updateReq.Confirmed && outage.ConfirmedAt.Valid {
			outage.ConfirmedAt = sql.NullTime{Valid: false}
		}
	}
	if message, valid := outage.Validate(); !valid {
		respondWithError(w, http.StatusBadRequest, message)
		return
	}

	if err := h.outageManager.UpdateOutage(outage, activeUser); err != nil {
		logger.WithField("error", err).Error("Failed to update outage in database")
		respondWithError(w, http.StatusInternalServerError, "Failed to update outage")
		return
	}

	logger.Info("Successfully updated outage")

	respondWithJSON(w, http.StatusOK, outage)
}

// GetOutageJSON retrieves a specific outage by ID for a specific sub-component.
func (h *Handlers) GetOutageJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-component not found")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	logger.Info("Successfully retrieved outage")
	respondWithJSON(w, http.StatusOK, outage)
}

// DeleteOutage deletes an outage by ID for a specific sub-component.
func (h *Handlers) DeleteOutage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-component not found")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to delete outage")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	if err := h.outageManager.DeleteOutage(outage, activeUser); err != nil {
		logger.WithField("error", err).Error("Failed to delete outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete outage")
		return
	}

	logger.Info("Successfully deleted outage")
	w.WriteHeader(http.StatusNoContent)
}

// AddTriageNoteJSON adds a triage note to an outage and posts it as a Slack thread reply.
func (h *Handlers) AddTriageNoteJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageID,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	var req types.TriageNoteBodyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to add triage note")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	// Scope the outage lookup to this component/sub-component to prevent cross-component access via guessed IDs.
	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	if strings.TrimSpace(req.Body) == "" {
		respondWithError(w, http.StatusBadRequest, "Body is required")
		return
	}

	note := &types.TriageNote{
		OutageID: uint(outageID),
		Body:     strings.TrimSpace(req.Body),
		Author:   activeUser,
	}

	if err := h.outageManager.AddTriageNote(note); err != nil {
		logger.WithField("error", err).Error("Failed to add triage note")
		respondWithError(w, http.StatusInternalServerError, "Failed to add triage note")
		return
	}

	logger.Info("Successfully added triage note")
	respondWithJSON(w, http.StatusCreated, note)
}

// resolveTriageNote is shared setup for triage note mutation handlers.
// It writes the appropriate error response and returns ok=false on any failure.
func (h *Handlers) resolveTriageNote(w http.ResponseWriter, r *http.Request) (outageID, noteID uint, activeUser string, logger *logrus.Entry, ok bool) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	activeUser, authOK := GetUserFromContext(r.Context())
	if !authOK {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return 0, 0, "", nil, false
	}

	rawOutageID, err := strconv.ParseUint(vars["outageId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return 0, 0, "", nil, false
	}

	rawNoteID, err := strconv.ParseUint(vars["noteId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid note ID")
		return 0, 0, "", nil, false
	}

	outageID = uint(rawOutageID)
	noteID = uint(rawNoteID)

	logger = h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageID,
		"note_id":       noteID,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return 0, 0, "", nil, false
	}

	if component.GetSubComponentBySlug(subComponentName) == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return 0, 0, "", nil, false
	}

	isAdmin := h.IsUserAuthorizedForComponent(activeUser, component)

	// Verify the outage belongs to this component/sub-component to prevent cross-component access.
	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, outageID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return 0, 0, "", nil, false
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return 0, 0, "", nil, false
	}

	note, err := h.triageNoteRepo.GetTriageNote(outageID, noteID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Triage note not found")
			return 0, 0, "", nil, false
		}
		logger.WithField("error", err).Error("Failed to query triage note from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get triage note")
		return 0, 0, "", nil, false
	}

	if !isAdmin && note.Author != activeUser {
		logger.Warn("User not authorized to modify triage note")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action")
		return 0, 0, "", nil, false
	}

	return outageID, noteID, activeUser, logger, true
}

// UpdateTriageNoteJSON updates the body of a triage note. Allowed for component admins and the note author.
func (h *Handlers) UpdateTriageNoteJSON(w http.ResponseWriter, r *http.Request) {
	var req types.TriageNoteBodyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	outageID, noteID, activeUser, logger, ok := h.resolveTriageNote(w, r)
	if !ok {
		return
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		respondWithError(w, http.StatusBadRequest, "Body is required")
		return
	}

	updated, err := h.outageManager.UpdateTriageNote(outageID, noteID, body, activeUser)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Triage note not found")
			return
		}
		logger.WithField("error", err).Error("Failed to update triage note")
		respondWithError(w, http.StatusInternalServerError, "Failed to update triage note")
		return
	}

	logger.Info("Successfully updated triage note")
	respondWithJSON(w, http.StatusOK, updated)
}

// DeleteTriageNoteJSON removes a triage note. Allowed for component admins and the note author.
func (h *Handlers) DeleteTriageNoteJSON(w http.ResponseWriter, r *http.Request) {
	outageID, noteID, activeUser, logger, ok := h.resolveTriageNote(w, r)
	if !ok {
		return
	}

	if err := h.outageManager.DeleteTriageNote(outageID, noteID, activeUser); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Triage note not found")
			return
		}
		logger.WithField("error", err).Error("Failed to delete triage note")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete triage note")
		return
	}

	logger.Info("Successfully deleted triage note")
	w.WriteHeader(http.StatusNoContent)
}

// AddOutageLinkJSON adds a URL link to an outage.
func (h *Handlers) AddOutageLinkJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageID,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	var req types.OutageLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to add outage link")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	// Scope the outage lookup to this component/sub-component to prevent cross-component access via guessed IDs.
	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		respondWithError(w, http.StatusBadRequest, "URL is required")
		return
	}
	if _, ok := utils.ParseHTTPURL(rawURL); !ok {
		respondWithError(w, http.StatusBadRequest, "URL must use http or https")
		return
	}

	linkType := types.LinkType(req.LinkType)
	if linkType == "" {
		linkType = types.LinkTypeOther
	} else if !types.IsValidLinkType(req.LinkType) {
		respondWithError(w, http.StatusBadRequest, "Invalid link type")
		return
	}

	// Description is only meaningful for the "other" type.
	description := ""
	if linkType == types.LinkTypeOther {
		description = strings.TrimSpace(req.Description)
	}

	link := &types.OutageLink{
		OutageID:    uint(outageID),
		URL:         rawURL,
		LinkType:    linkType,
		Description: description,
	}

	if err := h.outageManager.AddOutageLink(link, activeUser); err != nil {
		logger.WithField("error", err).Error("Failed to add outage link")
		respondWithError(w, http.StatusInternalServerError, "Failed to add outage link")
		return
	}

	logger.Info("Successfully added outage link")
	respondWithJSON(w, http.StatusCreated, link)
}

// resolveOutageLink is shared setup for outage link mutation handlers.
// It writes the appropriate error response and returns ok=false on any failure.
func (h *Handlers) resolveOutageLink(w http.ResponseWriter, r *http.Request) (outageID, linkID uint, activeUser string, logger *logrus.Entry, ok bool) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	activeUser, authOk := GetUserFromContext(r.Context())
	if !authOk {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return 0, 0, "", nil, false
	}

	parsedOutageID, err := strconv.ParseUint(vars["outageId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return 0, 0, "", nil, false
	}

	parsedLinkID, err := strconv.ParseUint(vars["linkId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid link ID")
		return 0, 0, "", nil, false
	}

	logger = h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     parsedOutageID,
		"link_id":       parsedLinkID,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return 0, 0, "", nil, false
	}

	if component.GetSubComponentBySlug(subComponentName) == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return 0, 0, "", nil, false
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to modify outage link")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return 0, 0, "", nil, false
	}

	// Scope the outage lookup to this component/sub-component to prevent cross-component access via guessed IDs.
	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(parsedOutageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return 0, 0, "", nil, false
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return 0, 0, "", nil, false
	}

	return uint(parsedOutageID), uint(parsedLinkID), activeUser, logger, true
}

// UpdateOutageLinkJSON updates an existing outage link's URL, type, and description.
func (h *Handlers) UpdateOutageLinkJSON(w http.ResponseWriter, r *http.Request) {
	var req types.OutageLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	outageID, linkID, activeUser, logger, ok := h.resolveOutageLink(w, r)
	if !ok {
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		respondWithError(w, http.StatusBadRequest, "URL is required")
		return
	}
	if _, ok := utils.ParseHTTPURL(rawURL); !ok {
		respondWithError(w, http.StatusBadRequest, "URL must use http or https")
		return
	}

	linkType := types.LinkType(req.LinkType)
	if linkType == "" {
		linkType = types.LinkTypeOther
	} else if !types.IsValidLinkType(req.LinkType) {
		respondWithError(w, http.StatusBadRequest, "Invalid link type")
		return
	}

	description := ""
	if linkType == types.LinkTypeOther {
		description = strings.TrimSpace(req.Description)
	}

	link, err := h.outageManager.UpdateOutageLink(outageID, linkID, rawURL, linkType, description, activeUser)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Link not found")
			return
		}
		logger.WithField("error", err).Error("Failed to update outage link")
		respondWithError(w, http.StatusInternalServerError, "Failed to update outage link")
		return
	}

	logger.Info("Successfully updated outage link")
	respondWithJSON(w, http.StatusOK, link)
}

// DeleteOutageLinkJSON removes a link from an outage.
func (h *Handlers) DeleteOutageLinkJSON(w http.ResponseWriter, r *http.Request) {
	outageID, linkID, activeUser, logger, ok := h.resolveOutageLink(w, r)
	if !ok {
		return
	}

	if err := h.outageManager.DeleteOutageLink(outageID, linkID, activeUser); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Link not found")
			return
		}
		logger.WithField("error", err).Error("Failed to delete outage link")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete outage link")
		return
	}

	logger.Info("Successfully deleted outage link")
	w.WriteHeader(http.StatusNoContent)
}

// GetOutageRelationshipsJSON returns all relationships for a given outage.
func (h *Handlers) GetOutageRelationshipsJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
	})

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	respondWithJSON(w, http.StatusOK, outage.Relationships)
}

// AddOutageRelationshipJSON creates a relationship between two outages.
func (h *Handlers) AddOutageRelationshipJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageID,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	if component.GetSubComponentBySlug(subComponentName) == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to add outage relationship")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	var req types.OutageRelationshipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RelatedOutageID == 0 {
		respondWithError(w, http.StatusBadRequest, "related_outage_id is required")
		return
	}

	if req.RelatedOutageID == uint(outageID) {
		respondWithError(w, http.StatusBadRequest, "Cannot create a relationship to itself")
		return
	}

	if !types.IsValidRelationshipType(req.RelationshipType) {
		respondWithError(w, http.StatusBadRequest, "Invalid relationship type. Must be one of: causes, caused_by, related_to")
		return
	}

	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	exists, err := h.outageManager.OutageExists(req.RelatedOutageID)
	if err != nil {
		logger.WithField("error", err).Error("Failed to check related outage existence")
		respondWithError(w, http.StatusInternalServerError, "Failed to validate related outage")
		return
	}
	if !exists {
		respondWithError(w, http.StatusBadRequest, "Related outage not found")
		return
	}

	for _, e := range outage.Relationships {
		if e.RelatedOutageID == req.RelatedOutageID {
			respondWithError(w, http.StatusConflict, "A relationship between these outages already exists")
			return
		}
	}

	rel := &types.OutageRelationship{
		OutageID:         uint(outageID),
		RelatedOutageID:  req.RelatedOutageID,
		RelationshipType: types.RelationshipType(req.RelationshipType),
	}

	result, err := h.outageManager.AddOutageRelationship(rel, activeUser)
	if err != nil {
		logger.WithField("error", err).Error("Failed to add outage relationship")
		respondWithError(w, http.StatusInternalServerError, "Failed to add outage relationship")
		return
	}

	logger.Info("Successfully added outage relationship")
	respondWithJSON(w, http.StatusCreated, result)
}

// DeleteOutageRelationshipJSON removes a relationship between two outages.
func (h *Handlers) DeleteOutageRelationshipJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	activeUser, authOk := GetUserFromContext(r.Context())
	if !authOk {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	parsedOutageID, err := strconv.ParseUint(vars["outageId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	parsedRelID, err := strconv.ParseUint(vars["relationshipId"], 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid relationship ID")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":       componentName,
		"sub_component":   subComponentName,
		"outage_id":       parsedOutageID,
		"relationship_id": parsedRelID,
		"active_user":     activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	if component.GetSubComponentBySlug(subComponentName) == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	if !h.IsUserAuthorizedForComponent(activeUser, component) {
		logger.Warn("User not authorized to delete outage relationship")
		respondWithError(w, http.StatusForbidden, "You are not authorized to perform this action on this component")
		return
	}

	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(parsedOutageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	if err := h.outageManager.DeleteOutageRelationship(uint(parsedOutageID), uint(parsedRelID), activeUser); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Relationship not found")
			return
		}
		logger.WithField("error", err).Error("Failed to delete outage relationship")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete outage relationship")
		return
	}

	logger.Info("Successfully deleted outage relationship")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetOutageAuditLogsJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
	})

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}
	// Get the Outage using the component and subComponents to verify that the outage belongs to them
	outage, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	auditLogs, err := h.outageManager.GetOutageAuditLogs(outage.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage audit logs")
		return
	}

	respondWithJSON(w, http.StatusOK, auditLogs)
}

// GetTriageNotesJSON returns all triage notes for a given outage.
func (h *Handlers) GetTriageNotesJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
	})

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	notes, err := h.triageNoteRepo.ListTriageNotes(uint(outageID))
	if err != nil {
		logger.WithField("error", err).Error("Failed to list triage notes")
		respondWithError(w, http.StatusInternalServerError, "Failed to get triage notes")
		return
	}

	respondWithJSON(w, http.StatusOK, notes)
}

// GetOutageLinksJSON returns all links for a given outage.
func (h *Handlers) GetOutageLinksJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]
	outageIDStr := vars["outageId"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"outage_id":     outageIDStr,
	})

	outageID, err := strconv.ParseUint(outageIDStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid outage ID")
		return
	}

	if _, err := h.outageManager.GetOutageByID(componentName, subComponentName, uint(outageID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondWithError(w, http.StatusNotFound, "Outage not found")
			return
		}
		logger.WithField("error", err).Error("Failed to query outage from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage")
		return
	}

	links, err := h.outageLinkRepo.ListOutageLinks(uint(outageID))
	if err != nil {
		logger.WithField("error", err).Error("Failed to list outage links")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outage links")
		return
	}

	respondWithJSON(w, http.StatusOK, links)
}

// GetSubComponentStatusJSON returns the status of a subcomponent based on active outages
func (h *Handlers) GetSubComponentStatusJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-component not found")
		return
	}

	active, err := h.statusForSubComponent(componentName, subComponentName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get subcomponent status")
		return
	}

	lastPingTime, err := h.pingRepo.GetLastPingTime(componentName, subComponentName)
	if err != nil {
		logger.WithField("error", err).Warn("Failed to query component report ping")
	}

	response := types.ComponentStatus{
		ComponentName: fmt.Sprintf("%s/%s", componentName, subComponentName),
		Status:        active.Status,
		ActiveOutages: active.Confirmed,
		LastPingTime:  lastPingTime,
	}

	if len(active.Suspected) > 0 {
		s := active.Suspected[0]
		reporters := make([]string, len(s.Reports))
		for i, r := range s.Reports {
			reporters[i] = r.User
		}
		response.SuspectedOutage = &types.SuspectedOutageInfo{
			OutageID:    s.ID,
			ReportCount: int64(len(s.Reports)),
			Description: s.Description,
			StartTime:   s.StartTime,
			Reporters:   reporters,
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetComponentStatusJSON returns the status of a component based on active outages in all its sub-components
func (h *Handlers) GetComponentStatusJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]

	logger := h.logger.WithField("component", componentName)

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}

	response, err := h.getComponentStatus(component, logger)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get component status")
		return
	}
	respondWithJSON(w, http.StatusOK, response)
}

// GetAllComponentsStatusJSON returns the status of all components
func (h *Handlers) GetAllComponentsStatusJSON(w http.ResponseWriter, r *http.Request) {
	logger := h.logger

	var allComponentStatuses []types.ComponentStatus

	for _, component := range h.config().Components {
		componentLogger := logger.WithField("component", component.Name)
		componentStatus, err := h.getComponentStatus(component, componentLogger)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Failed to get component status")
			return
		}

		allComponentStatuses = append(allComponentStatuses, componentStatus)
	}
	respondWithJSON(w, http.StatusOK, allComponentStatuses)
}

// getComponentStatus calculates the status of a component based on its sub-components and active outages
func (h *Handlers) getComponentStatus(component *types.Component, logger *logrus.Entry) (types.ComponentStatus, error) {
	confirmed, err := h.outageManager.GetActiveOutagesForComponent(component.Slug)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query active outages from database")
		return types.ComponentStatus{}, err
	}

	suspected, err := h.outageManager.GetActiveSuspectedOutagesForComponent(component.Slug)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query suspected outages from database")
		return types.ComponentStatus{}, err
	}

	subComponentsWithOutages := make(map[string]bool)
	for _, outage := range confirmed {
		subComponentsWithOutages[outage.SubComponentName] = true
	}

	var criticalOutages []types.Outage
	for _, outage := range confirmed {
		sub := component.GetSubComponentBySlug(outage.SubComponentName)
		if sub != nil && sub.Critical {
			criticalOutages = append(criticalOutages, outage)
		}
	}

	isPartialOutage := len(subComponentsWithOutages) < len(component.Subcomponents)

	var status types.Status
	if len(confirmed) == 0 && len(suspected) > 0 {
		status = types.StatusSuspected
	} else if len(confirmed) == 0 {
		status = types.StatusHealthy
	} else if len(criticalOutages) > 0 && isPartialOutage {
		status = types.StatusFromOutages(criticalOutages)
	} else if isPartialOutage {
		status = types.StatusPartial
	} else {
		status = types.StatusFromOutages(confirmed)
	}

	lastPingTime, err := h.pingRepo.GetMostRecentPingTimeForAnySubComponent(component.Slug)
	if err != nil {
		logger.WithField("error", err).Warn("Failed to query component report pings")
	}

	suspectedBySubComponent := make(map[string][]types.Outage)
	for _, o := range suspected {
		suspectedBySubComponent[o.SubComponentName] = append(suspectedBySubComponent[o.SubComponentName], o)
	}
	confirmedBySubComponent := make(map[string][]types.Outage)
	for _, o := range confirmed {
		confirmedBySubComponent[o.SubComponentName] = append(confirmedBySubComponent[o.SubComponentName], o)
	}
	subComponentStatuses := make(map[string]types.Status, len(component.Subcomponents))
	for _, sub := range component.Subcomponents {
		subComponentStatuses[sub.Slug] = types.StatusFromActiveOutages(
			confirmedBySubComponent[sub.Slug],
			suspectedBySubComponent[sub.Slug],
		)
	}

	return types.ComponentStatus{
		ComponentName:        component.Name,
		Status:               status,
		ActiveOutages:        confirmed,
		LastPingTime:         lastPingTime,
		SubComponentStatuses: subComponentStatuses,
	}, nil
}

// GetSubComponentHistoryJSON returns day-bucketed outage history for a sub-component.
// Query param: days (int, default 90, max 365).
func (h *Handlers) GetSubComponentHistoryJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentSlug := vars["componentName"]
	subComponentSlug := vars["subComponentName"]

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentSlug,
		"sub_component": subComponentSlug,
	})

	component := h.config().GetComponentBySlug(componentSlug)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}
	if component.GetSubComponentBySlug(subComponentSlug) == nil {
		respondWithError(w, http.StatusNotFound, "Sub-component not found")
		return
	}

	days := 90
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		providedDays, err := strconv.Atoi(daysStr)
		if err != nil || providedDays <= 0 {
			respondWithError(w, http.StatusBadRequest, "days must be a positive integer")
			return
		}
		if providedDays > 365 {
			respondWithError(w, http.StatusBadRequest, "days must not exceed 365")
			return
		}
		days = providedDays
	}

	now := time.Now().UTC()
	queryStart := now.AddDate(0, 0, -days)
	refs := []types.SubComponentRef{{ComponentSlug: componentSlug, SubSlug: subComponentSlug}}

	outages, err := h.outageManager.GetOutagesDuring(queryStart, now, refs)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query outage history from database")
		respondWithError(w, http.StatusInternalServerError, "Failed to get history")
		return
	}

	respondWithJSON(w, http.StatusOK, buildHistoryBuckets(outages, days, now))
}

// ListTagsJSON returns the list of configured tags.
func (h *Handlers) ListTagsJSON(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, h.config().Tags)
}

// parseStatusFilters parses repeated and/or comma-separated status query values.
// Returns nil when no status filter was provided.
func parseStatusFilters(raw []string) ([]types.Status, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	seen := make(map[types.Status]bool)
	var result []types.Status
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !types.IsValidSubComponentStatus(part) {
				return nil, fmt.Sprintf("invalid status: %s", part)
			}
			s := types.Status(part)
			if seen[s] {
				continue
			}
			seen[s] = true
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil, "status filter must include at least one status"
	}
	return result, ""
}

// ListSubComponentsJSON handles HTTP requests to fetch a list of sub-components based on filters like componentName, team, tag, or status.
// All must be matched for a sub-component to be returned. If no filters are provided, all sub-components are returned.
// The status query parameter may be repeated and/or comma-separated (e.g. status=Down&status=Degraded or status=Down,Degraded).
// Each item includes the current sub-component status.
func (h *Handlers) ListSubComponentsJSON(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	componentSlug := q.Get("componentName")
	tag := q.Get("tag")
	team := q.Get("team")
	statusFilters, errMsg := parseStatusFilters(q["status"])
	if errMsg != "" {
		respondWithError(w, http.StatusBadRequest, errMsg)
		return
	}

	refs := h.config().SubComponentRefsMatching(componentSlug, "", tag, team)

	confirmedByRef, suspectedByRef, err := h.activeOutagesByRef(refs)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get sub-component status")
		return
	}

	statusSet := make(map[types.Status]bool, len(statusFilters))
	for _, s := range statusFilters {
		statusSet[s] = true
	}

	items := make([]types.SubComponentListItem, 0, len(refs))
	for _, ref := range refs {
		component := h.config().GetComponentBySlug(ref.ComponentSlug)
		if component == nil || component.SLOComponent {
			continue
		}
		sub := component.GetSubComponentBySlug(ref.SubSlug)
		if sub == nil {
			continue
		}
		st := types.StatusFromActiveOutages(confirmedByRef[ref], suspectedByRef[ref])
		if len(statusFilters) > 0 && !statusSet[st] {
			continue
		}
		items = append(items, types.SubComponentListItem{
			ComponentName: component.Name,
			SubComponent:  *sub,
			Status:        st,
		})
	}

	respondWithJSON(w, http.StatusOK, items)
}

// activeOutagesByRef loads active confirmed and suspected outages once per unique component
// among refs, grouped by SubComponentRef.
func (h *Handlers) activeOutagesByRef(refs []types.SubComponentRef) (confirmedByRef, suspectedByRef map[types.SubComponentRef][]types.Outage, err error) {
	confirmedByRef = make(map[types.SubComponentRef][]types.Outage)
	suspectedByRef = make(map[types.SubComponentRef][]types.Outage)
	seenComponents := make(map[string]bool)
	for _, ref := range refs {
		if seenComponents[ref.ComponentSlug] {
			continue
		}
		seenComponents[ref.ComponentSlug] = true

		confirmed, err := h.outageManager.GetActiveOutagesForComponent(ref.ComponentSlug)
		if err != nil {
			h.logger.WithFields(logrus.Fields{
				"component": ref.ComponentSlug,
				"error":     err,
			}).Error("Failed to query active outages")
			return nil, nil, err
		}
		for _, o := range confirmed {
			r := types.SubComponentRef{ComponentSlug: o.ComponentName, SubSlug: o.SubComponentName}
			confirmedByRef[r] = append(confirmedByRef[r], o)
		}

		suspected, err := h.outageManager.GetActiveSuspectedOutagesForComponent(ref.ComponentSlug)
		if err != nil {
			h.logger.WithFields(logrus.Fields{
				"component": ref.ComponentSlug,
				"error":     err,
			}).Error("Failed to query suspected outages")
			return nil, nil, err
		}
		for _, o := range suspected {
			r := types.SubComponentRef{ComponentSlug: o.ComponentName, SubSlug: o.SubComponentName}
			suspectedByRef[r] = append(suspectedByRef[r], o)
		}
	}
	return confirmedByRef, suspectedByRef, nil
}

// subComponentActiveStatus holds status and active outages for a single sub-component.
type subComponentActiveStatus struct {
	Status    types.Status
	Confirmed []types.Outage
	Suspected []types.Outage
}

// statusForSubComponent loads active confirmed and suspected outages for a sub-component and derives its status.
func (h *Handlers) statusForSubComponent(componentSlug, subSlug string) (subComponentActiveStatus, error) {
	confirmed, err := h.outageManager.GetActiveOutagesForSubComponent(componentSlug, subSlug)
	if err != nil {
		h.logger.WithFields(logrus.Fields{
			"component":     componentSlug,
			"sub_component": subSlug,
			"error":         err,
		}).Error("Failed to query active outages")
		return subComponentActiveStatus{}, err
	}
	suspected, err := h.outageManager.GetActiveSuspectedOutages(componentSlug, subSlug)
	if err != nil {
		h.logger.WithFields(logrus.Fields{
			"component":     componentSlug,
			"sub_component": subSlug,
			"error":         err,
		}).Error("Failed to query suspected outages")
		return subComponentActiveStatus{}, err
	}
	return subComponentActiveStatus{
		Status:    types.StatusFromActiveOutages(confirmed, suspected),
		Confirmed: confirmed,
		Suspected: suspected,
	}, nil
}

// GetOutagesDuringJSON returns outages overlapping the requested time window (or a single instant when only one of start/end is set).
func (h *Handlers) GetOutagesDuringJSON(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")
	componentSlug := q.Get("componentName")
	subSlug := q.Get("subComponentName")
	tag := q.Get("tag")
	team := q.Get("team")

	logger := h.logger.WithFields(logrus.Fields{
		"start":            startStr,
		"end":              endStr,
		"componentName":    componentSlug,
		"subComponentName": subSlug,
		"tag":              tag,
		"team":             team,
	})

	if startStr == "" && endStr == "" {
		respondWithError(w, http.StatusBadRequest, "at least one of start or end is required (RFC3339 or RFC3339Nano)")
		return
	}
	if subSlug != "" && componentSlug == "" {
		respondWithError(w, http.StatusBadRequest, "componentName is required when subComponentName is set")
		return
	}

	queryStart, queryEnd, errMsg := utils.OutagesDuringQueryBounds(startStr, endStr)
	if errMsg != "" {
		respondWithError(w, http.StatusBadRequest, errMsg)
		return
	}

	if componentSlug != "" {
		component := h.config().GetComponentBySlug(componentSlug)
		if component == nil {
			respondWithError(w, http.StatusNotFound, "Component not found")
			return
		}
		if subSlug != "" && component.GetSubComponentBySlug(subSlug) == nil {
			respondWithError(w, http.StatusNotFound, "Sub-component not found")
			return
		}
	}

	refs := h.config().SubComponentRefsMatching(componentSlug, subSlug, tag, team)
	outages, err := h.outageManager.GetOutagesDuring(queryStart, queryEnd, refs)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query outages during window")
		respondWithError(w, http.StatusInternalServerError, "Failed to get outages")
		return
	}
	if outages == nil {
		outages = []types.Outage{}
	}
	respondWithJSON(w, http.StatusOK, outages)
}

func (h *Handlers) PostComponentMonitorReportJSON(w http.ResponseWriter, r *http.Request) {
	var req types.ComponentMonitorReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ComponentMonitor == "" {
		respondWithError(w, http.StatusBadRequest, "component_monitor is required")
		return
	}

	if len(req.Statuses) == 0 {
		respondWithError(w, http.StatusBadRequest, "statuses cannot be empty")
		return
	}

	for _, status := range req.Statuses {
		component := h.config().GetComponentBySlug(status.ComponentSlug)
		if component == nil {
			respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Component not found: %s", status.ComponentSlug))
			return
		}

		subComponent := component.GetSubComponentBySlug(status.SubComponentSlug)
		if subComponent == nil {
			respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Sub-component not found: %s/%s", status.ComponentSlug, status.SubComponentSlug))
			return
		}
	}

	user, authenticated := GetUserFromContext(r.Context())
	if !authenticated {
		respondWithError(w, http.StatusUnauthorized, "no Authenticated ServiceAccount user found")
		return
	}
	err := h.monitorReportProcessor.ValidateRequest(&req, user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	err = h.monitorReportProcessor.Process(&req)
	if err != nil {
		h.logger.WithField("error", err).Error("Failed to process component monitor report")
		respondWithError(w, http.StatusInternalServerError, "Failed to process report")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "processed"})
}

type AuthenticatedUser struct {
	Username   string   `json:"username" yaml:"username"`
	Components []string `json:"components" yaml:"components"`
	TeamSLOs   []string `json:"team_slos" yaml:"team_slos"`
}

func (h *Handlers) GetAuthenticatedUserJSON(w http.ResponseWriter, r *http.Request) {
	user, authenticated := GetUserFromContext(r.Context())
	if !authenticated {
		respondWithError(w, http.StatusUnauthorized, "No Authenticated user found")
		return
	}

	response := AuthenticatedUser{
		Username:   user,
		Components: []string{},
		TeamSLOs:   []string{},
	}

	// Return only components the user is authorized for
	for _, component := range h.config().Components {
		if h.IsUserAuthorizedForComponent(user, component) {
			response.Components = append(response.Components, component.Slug)
		}
	}
	for _, team := range h.config().TeamSLOs {
		if h.IsUserAuthorizedForTeamSLO(user, team.Team) {
			response.TeamSLOs = append(response.TeamSLOs, team.Team)
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

type reportSuspectedResponse struct {
	Outage      *types.Outage `json:"outage"`
	ReportCount int64         `json:"report_count"`
	Created     bool          `json:"created"`
}

// ReportSuspectedOutageJSON handles community suspected-outage reports from authenticated non-admin users.
func (h *Handlers) ReportSuspectedOutageJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["componentName"]
	subComponentName := vars["subComponentName"]

	activeUser, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return
	}

	logger := h.logger.WithFields(logrus.Fields{
		"component":     componentName,
		"sub_component": subComponentName,
		"active_user":   activeUser,
	})

	component := h.config().GetComponentBySlug(componentName)
	if component == nil {
		respondWithError(w, http.StatusNotFound, "Component not found")
		return
	}
	subComponent := component.GetSubComponentBySlug(subComponentName)
	if subComponent == nil {
		respondWithError(w, http.StatusNotFound, "Sub-Component not found")
		return
	}

	var req types.ReportSuspectedOutageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	activeOutages, err := h.outageManager.GetActiveOutagesForSubComponent(componentName, subComponentName)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query active outages")
		respondWithError(w, http.StatusInternalServerError, "Failed to process report")
		return
	}
	if len(activeOutages) > 0 {
		respondWithError(w, http.StatusConflict, "An outage is already being tracked for this component")
		return
	}

	suspected, err := h.outageManager.GetActiveSuspectedOutages(componentName, subComponentName)
	if err != nil {
		logger.WithField("error", err).Error("Failed to query suspected outages")
		respondWithError(w, http.StatusInternalServerError, "Failed to process report")
		return
	}
	if len(suspected) > 0 {
		for _, r := range suspected[0].Reports {
			if r.User == activeUser {
				respondWithError(w, http.StatusConflict, "You have already reported this outage")
				return
			}
		}
	}

	result, err := h.outageManager.ReportSuspectedOutage(componentName, subComponentName, strings.TrimSpace(req.Description), activeUser, subComponent.ReportThreshold)
	if err != nil {
		logger.WithField("error", err).Error("Failed to process suspected outage report")
		respondWithError(w, http.StatusInternalServerError, "Failed to process report")
		return
	}

	logger.WithField("outage_id", result.Outage.ID).Info("Successfully processed community suspected outage report")

	respondWithJSON(w, http.StatusCreated, reportSuspectedResponse{
		Outage:      result.Outage,
		ReportCount: result.ReportCount,
		Created:     result.Created,
	})
}

type upsertSLOItemRequest struct {
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	ItemKey       string          `json:"item_key"`
	GroupKey      string          `json:"group_key"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Outcome       string          `json:"outcome"`
	Details       json.RawMessage `json:"details"`
	Notes         string          `json:"notes"`
}

func (req *upsertSLOItemRequest) validate(team *types.TeamSLOConfig) string {
	if team == nil {
		return "team has no workspace for this kind"
	}
	ws := team.Workspace()
	if ws == nil || ws.Kind != strings.TrimSpace(req.Kind) {
		return "team has no workspace for this kind"
	}
	if req.SchemaVersion <= 0 {
		return "schema_version is required"
	}
	if !slo.KnownWorkspace(req.Kind, req.SchemaVersion) {
		return "unknown schema_version"
	}
	if strings.TrimSpace(req.ItemKey) == "" {
		return "item_key is required"
	}
	if strings.TrimSpace(req.GroupKey) == "" {
		return "group_key is required"
	}
	if req.OccurredAt.IsZero() {
		return "occurred_at is required"
	}
	if strings.TrimSpace(req.Outcome) == "" {
		return "outcome is required"
	}
	return ""
}

type addSLOLinkRequest struct {
	URL      string `json:"url"`
	LinkType string `json:"link_type"`
	OutageID *uint  `json:"outage_id,omitempty"`
}

type sloItemView struct {
	ID            uint                     `json:"id"`
	Kind          string                   `json:"kind"`
	SchemaVersion int                      `json:"schema_version"`
	ItemKey       string                   `json:"item_key"`
	GroupKey      string                   `json:"group_key"`
	OccurredAt    time.Time                `json:"occurred_at"`
	Outcome       string                   `json:"outcome"`
	Details       json.RawMessage          `json:"details"`
	Notes         string                   `json:"notes"`
	UpdatedBy     string                   `json:"updated_by"`
	Links         []types.SLOWorkspaceLink `json:"links"`
}

type teamSLOView struct {
	Team          string                   `json:"team"`
	Workspace     *types.SLOWorkspace      `json:"workspace,omitempty"`
	Evaluations   []slo.Evaluation         `json:"evaluations"`
	SLOComponents []types.SLOComponentView `json:"slo_components"`
	Items         []sloItemView            `json:"items"`
}

type teamSLOSummaryTeam struct {
	Team          string                   `json:"team"`
	Evaluations   []slo.Evaluation         `json:"evaluations"`
	SLOComponents []types.SLOComponentView `json:"slo_components"`
}

type teamSLOSummary struct {
	Teams []teamSLOSummaryTeam `json:"teams"`
}

func toSLOItemViews(items []types.SLOWorkspaceItem) []sloItemView {
	out := make([]sloItemView, 0, len(items))
	for _, item := range items {
		links := item.Links
		if links == nil {
			links = []types.SLOWorkspaceLink{}
		}
		details := json.RawMessage(item.Details)
		if len(details) == 0 {
			details = json.RawMessage(`{}`)
		}
		out = append(out, sloItemView{
			ID:            item.ID,
			Kind:          item.Kind,
			SchemaVersion: item.SchemaVersion,
			ItemKey:       item.ItemKey,
			GroupKey:      item.GroupKey,
			OccurredAt:    item.OccurredAt.UTC(),
			Outcome:       item.Outcome,
			Details:       details,
			Notes:         item.Notes,
			UpdatedBy:     item.UpdatedBy,
			Links:         links,
		})
	}
	return out
}

func (h *Handlers) IsUserAuthorizedForTeamSLO(user, team string) bool {
	cfg := h.config().TeamSLOByTeam(team)
	if cfg == nil {
		return false
	}
	return slices.Contains(h.collectOwnerIdentities(cfg.Owners), user)
}

func (h *Handlers) requireTeamSLOWriter(w http.ResponseWriter, r *http.Request, team string) (string, bool) {
	user, ok := GetUserFromContext(r.Context())
	if !ok || user == "" {
		respondWithError(w, http.StatusUnauthorized, "no active user found")
		return "", false
	}
	if !h.IsUserAuthorizedForTeamSLO(user, team) {
		respondWithError(w, http.StatusForbidden, "not authorized for team SLO")
		return "", false
	}
	return user, true
}

func (h *Handlers) GetTeamSLOJSON(w http.ResponseWriter, r *http.Request) {
	team := mux.Vars(r)["team"]
	view, err := h.loadTeamView(team)
	if err != nil {
		h.logger.WithFields(logrus.Fields{"team": team, "error": err}).Error("Failed to load team SLO")
		respondWithError(w, http.StatusInternalServerError, "Failed to load team SLO")
		return
	}
	respondWithJSON(w, http.StatusOK, view)
}

func (h *Handlers) GetTeamSLOSummaryJSON(w http.ResponseWriter, r *http.Request) {
	names := h.config().SummaryTeamNames()
	summary := teamSLOSummary{Teams: []teamSLOSummaryTeam{}}
	for _, team := range names {
		view, err := h.loadTeamView(team)
		if err != nil {
			h.logger.WithFields(logrus.Fields{"team": team, "error": err}).Error("Failed to load team SLO summary")
			respondWithError(w, http.StatusInternalServerError, "Failed to load SLO summary")
			return
		}
		summary.Teams = append(summary.Teams, teamSLOSummaryTeam{
			Team:          team,
			Evaluations:   view.Evaluations,
			SLOComponents: view.SLOComponents,
		})
	}
	respondWithJSON(w, http.StatusOK, summary)
}

func (h *Handlers) loadTeamView(team string) (teamSLOView, error) {
	items, err := h.sloRepo.ListByTeam(team)
	if err != nil {
		return teamSLOView{}, err
	}
	cfg := h.config()
	teamCfg := cfg.TeamSLOByTeam(team)
	var ws *types.SLOWorkspace
	if teamCfg != nil {
		ws = teamCfg.Workspace()
	}
	outages, err := h.activeSLOComponentOutages(team)
	if err != nil {
		return teamSLOView{}, err
	}
	evaluations, err := slo.Evaluate(time.Now().UTC(), teamCfg, items)
	if err != nil {
		return teamSLOView{}, err
	}
	return teamSLOView{
		Team:          team,
		Workspace:     ws,
		Evaluations:   evaluations,
		SLOComponents: cfg.SLOComponentsForTeam(team, outages),
		Items:         toSLOItemViews(items),
	}, nil
}

func (h *Handlers) activeSLOComponentOutages(team string) (map[types.SubComponentRef][]types.Outage, error) {
	out := map[types.SubComponentRef][]types.Outage{}
	cfg := h.config()
	teamCfg := cfg.TeamSLOByTeam(team)
	if teamCfg == nil {
		return out, nil
	}
	for _, slug := range teamCfg.SLOComponents {
		component := cfg.GetComponentBySlug(slug)
		if component == nil || !component.SLOComponent {
			continue
		}
		active, err := h.outageManager.GetActiveOutagesForComponent(component.Slug)
		if err != nil {
			return nil, err
		}
		for _, item := range active {
			ref := types.SubComponentRef{ComponentSlug: item.ComponentName, SubSlug: item.SubComponentName}
			out[ref] = append(out[ref], item)
		}
	}
	return out, nil
}

func (h *Handlers) PutSLOItemJSON(w http.ResponseWriter, r *http.Request) {
	team := mux.Vars(r)["team"]
	user, ok := h.requireTeamSLOWriter(w, r, team)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	var req upsertSLOItemRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if msg := req.validate(h.config().TeamSLOByTeam(team)); msg != "" {
		respondWithError(w, http.StatusBadRequest, msg)
		return
	}
	if err := slo.ValidateDetails(req.Kind, req.SchemaVersion, req.Details); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := &types.SLOWorkspaceItem{
		Team:          team,
		Kind:          req.Kind,
		SchemaVersion: req.SchemaVersion,
		ItemKey:       strings.TrimSpace(req.ItemKey),
		GroupKey:      strings.TrimSpace(req.GroupKey),
		OccurredAt:    req.OccurredAt.UTC(),
		Outcome:       strings.TrimSpace(req.Outcome),
		Details:       []byte(req.Details),
		Notes:         req.Notes,
		UpdatedBy:     user,
	}
	stored, err := h.sloRepo.UpsertItem(item)
	if err != nil {
		h.logger.WithFields(logrus.Fields{"team": team, "item_key": item.ItemKey, "error": err}).Error("Failed to upsert SLO item")
		respondWithError(w, http.StatusInternalServerError, "Failed to save SLO item")
		return
	}
	respondWithJSON(w, http.StatusOK, toSLOItemViews([]types.SLOWorkspaceItem{*stored})[0])
}

func (h *Handlers) DeleteSLOItemJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	team := vars["team"]
	if _, ok := h.requireTeamSLOWriter(w, r, team); !ok {
		return
	}
	err := h.sloRepo.DeleteItem(team, vars["kind"], vars["itemKey"])
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondWithError(w, http.StatusNotFound, "SLO item not found")
		return
	}
	if err != nil {
		h.logger.WithField("error", err).Error("Failed to delete SLO item")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete SLO item")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) PutSLOItemLinkJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	team := vars["team"]
	user, ok := h.requireTeamSLOWriter(w, r, team)
	if !ok {
		return
	}
	var req addSLOLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		respondWithError(w, http.StatusBadRequest, "url is required")
		return
	}
	if !slo.ValidLinkType(req.LinkType) {
		respondWithError(w, http.StatusBadRequest, "link_type must be jira, outage, or other")
		return
	}
	items, err := h.sloRepo.ListByTeam(team)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to load SLO item")
		return
	}
	var itemID uint
	found := false
	for _, item := range items {
		if item.Kind == vars["kind"] && item.ItemKey == vars["itemKey"] {
			itemID = item.ID
			found = true
			break
		}
	}
	if !found {
		respondWithError(w, http.StatusNotFound, "SLO item not found")
		return
	}
	link, err := h.sloRepo.AddLink(&types.SLOWorkspaceLink{
		ItemID:   itemID,
		URL:      strings.TrimSpace(req.URL),
		LinkType: req.LinkType,
		OutageID: req.OutageID,
	})
	if err != nil {
		h.logger.WithFields(logrus.Fields{"team": team, "user": user, "error": err}).Error("Failed to add SLO link")
		respondWithError(w, http.StatusInternalServerError, "Failed to add SLO link")
		return
	}
	respondWithJSON(w, http.StatusOK, link)
}

func (h *Handlers) DeleteSLOItemLinkJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	team := vars["team"]
	if _, ok := h.requireTeamSLOWriter(w, r, team); !ok {
		return
	}
	linkID, err := strconv.ParseUint(vars["linkId"], 10, 64)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid link id")
		return
	}
	err = h.sloRepo.DeleteLink(team, vars["kind"], vars["itemKey"], uint(linkID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondWithError(w, http.StatusNotFound, "SLO link not found")
		return
	}
	if err != nil {
		h.logger.WithField("error", err).Error("Failed to delete SLO link")
		respondWithError(w, http.StatusInternalServerError, "Failed to delete SLO link")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
