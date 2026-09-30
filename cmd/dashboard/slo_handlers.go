package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
)

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

type addSLOLinkRequest struct {
	URL      string `json:"url"`
	LinkType string `json:"link_type"`
	OutageID *uint  `json:"outage_id,omitempty"`
}

func (h *Handlers) IsUserAuthorizedForTeamSLO(user, team string) bool {
	cfg := h.config().TeamSLOByTeam(team)
	if cfg == nil {
		return false
	}
	return slicesContains(h.collectOwnerIdentities(cfg.Owners), user)
}

func slicesContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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
	names := slo.SummaryTeamNames(h.config())
	summary := slo.Summary{Teams: []slo.SummaryTeam{}}
	for _, team := range names {
		view, err := h.loadTeamView(team)
		if err != nil {
			h.logger.WithFields(logrus.Fields{"team": team, "error": err}).Error("Failed to load team SLO summary")
			respondWithError(w, http.StatusInternalServerError, "Failed to load SLO summary")
			return
		}
		block := slo.SummaryTeam{
			Team:          team,
			Evaluations:   view.Evaluations,
			SLOComponents: view.SLOComponents,
		}
		summary.Teams = append(summary.Teams, block)
	}
	respondWithJSON(w, http.StatusOK, summary)
}

func (h *Handlers) loadTeamView(team string) (slo.TeamView, error) {
	items, err := h.sloRepo.ListByTeam(team)
	if err != nil {
		return slo.TeamView{}, err
	}
	cfg := h.config()
	teamCfg := cfg.TeamSLOByTeam(team)
	ws := teamCfg.Workspace()
	if slo.IsTRTPayloadWorkspace(ws) {
		// Hide rows retention will drop. Deletion runs in TRTPayloadPruner so this read stays side-effect free.
		items = slo.DropItems(items, slo.TRTPayloadPruneIDs(time.Now().UTC(), streamNames(ws), ws.RecentPayloads, items))
	}
	outages, err := h.activeSLOComponentOutages(team)
	if err != nil {
		return slo.TeamView{}, err
	}
	view := slo.TeamView{
		Team:          team,
		Evaluations:   slo.Evaluate(time.Now().UTC(), teamCfg, items),
		SLOComponents: slo.SLOComponentsForTeam(cfg, team, outages),
		Items:         []slo.ItemView{},
		ItemKeys:      []string{},
	}
	if slo.IsTRTPayloadWorkspace(ws) {
		view.Workspace = ws
		names := streamNames(ws)
		view.Items = slo.ToItemViews(slo.TRTPayloadDisplayItems(names, ws.RecentPayloads, items))
		view.ItemKeys = slo.TRTPayloadItemKeys(names, items)
	} else if ws != nil {
		view.Workspace = ws
	}
	return view, nil
}

func streamNames(ws *types.SLOWorkspace) []string {
	names := make([]string, 0, len(ws.Streams))
	for _, stream := range ws.Streams {
		names = append(names, stream.Name)
	}
	return names
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
		active, err := h.outageManager.GetActiveOutagesForComponent(component.EffectiveSlug())
		if err != nil {
			return nil, err
		}
		for _, outage := range active {
			ref := types.SubComponentRef{ComponentSlug: outage.ComponentName, SubSlug: outage.SubComponentName}
			out[ref] = append(out[ref], outage)
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
	if msg := validateUpsertSLOItem(h.config().TeamSLOByTeam(team), &req); msg != "" {
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
	respondWithJSON(w, http.StatusOK, slo.ToItemViews([]types.SLOWorkspaceItem{*stored})[0])
}

func validateUpsertSLOItem(team *types.TeamSLOConfig, req *upsertSLOItemRequest) string {
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
