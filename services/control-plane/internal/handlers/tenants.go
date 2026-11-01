package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

const (
	defaultMaxEventsPerDay      int64 = 1_000_000
	defaultMaxConcurrentQueries       = 10

	// Upper bounds keep a typo from creating an effectively unlimited tenant.
	maxEventsPerDayLimit      int64 = 1_000_000_000_000
	maxConcurrentQueriesLimit       = 1000
)

var (
	tenantStatuses = map[string]bool{"active": true, "suspended": true, "inactive": true}
	tenantRegions  = map[string]bool{"apac": true, "emea": true, "americas": true, "global": true}
	tenantPlans    = map[string]bool{"free": true, "growth": true, "enterprise": true}
	tenantBilling  = map[string]bool{"monthly": true, "annual": true}
)

const (
	defaultRegion       = "global"
	defaultPlanTier     = "growth"
	defaultBillingCycle = "monthly"

	maxTags        = 20
	maxTagLen      = 32
	maxMetadataLen = 8 * 1024
)

// profileInput carries the optional tenant profile fields of a request.
type profileInput struct {
	Region, PlanTier, BillingCycle *string
	Tags                           *[]string
	Metadata                       json.RawMessage
}

// validateProfile checks the profile fields that were supplied and returns the
// cleaned tags and metadata. Tags are trimmed, lower-cased and de-duplicated.
func validateProfile(in profileInput) (tags []string, metadata json.RawMessage, msg string) {
	if in.Region != nil && !tenantRegions[*in.Region] {
		return nil, nil, "region must be one of apac, emea, americas, global"
	}
	if in.PlanTier != nil && !tenantPlans[*in.PlanTier] {
		return nil, nil, "plan_tier must be one of free, growth, enterprise"
	}
	if in.BillingCycle != nil && !tenantBilling[*in.BillingCycle] {
		return nil, nil, "billing_cycle must be one of monthly, annual"
	}
	if in.Tags != nil {
		seen := map[string]bool{}
		tags = []string{}
		for _, t := range *in.Tags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" || utf8.RuneCountInString(t) > maxTagLen || strings.ContainsAny(t, "\x00\r\n,") {
				return nil, nil, "each tag must be 1-32 characters with no commas or line breaks"
			}
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
		if len(tags) > maxTags {
			return nil, nil, "at most 20 tags are allowed"
		}
	}
	if len(bytes.TrimSpace(in.Metadata)) > 0 {
		if len(in.Metadata) > maxMetadataLen {
			return nil, nil, "metadata must be at most 8 KB"
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(in.Metadata, &obj); err != nil || obj == nil {
			return nil, nil, "metadata must be a JSON object"
		}
		metadata = in.Metadata
	}
	return tags, metadata, ""
}

const tenantCols = `id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries,
	region, plan_tier, billing_cycle, tags, metadata, created_at`

func scanTenant(row interface{ Scan(...any) error }) (models.Tenant, error) {
	var t models.Tenant
	var q models.TenantQuota
	var meta []byte
	err := row.Scan(&t.ID, &t.OrganizationID, &t.ExternalTenantID, &t.Name, &t.Status,
		&q.MaxEventsPerDay, &q.MaxConcurrentQueries,
		&t.Region, &t.PlanTier, &t.BillingCycle, pq.Array(&t.Tags), &meta, &t.CreatedAt)
	t.Quota = &q
	t.Metadata = json.RawMessage(meta)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return t, err
}

// canonicalJSON re-encodes JSON so that two equal documents compare equal
// regardless of whitespace or key order.
func canonicalJSON(raw json.RawMessage) []byte {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// validateQuota checks only the fields that were supplied.
func validateQuota(q *models.UpdateTenantQuota) (string, bool) {
	if q == nil {
		return "", true
	}
	if q.MaxEventsPerDay != nil && (*q.MaxEventsPerDay < 1 || *q.MaxEventsPerDay > maxEventsPerDayLimit) {
		return "max_events_per_day must be between 1 and 1000000000000", false
	}
	if q.MaxConcurrentQueries != nil && (*q.MaxConcurrentQueries < 1 || *q.MaxConcurrentQueries > maxConcurrentQueriesLimit) {
		return "max_concurrent_queries must be between 1 and 1000", false
	}
	return "", true
}

func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" {
		if !tenantStatuses[v] {
			respondError(w, http.StatusBadRequest, "status must be one of active, suspended, inactive")
			return
		}
		status = &v
	}
	page := queryInt(r, "page", 1, 1, 1_000_000)
	perPage := queryInt(r, "per_page", 50, 1, 200)
	org := scopeOrgFilter(r, p)

	var total int
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM tenants
		WHERE ($1::text IS NULL OR organization_id = $1) AND ($2::text IS NULL OR status = $2)`,
		org, status).Scan(&total); err != nil {
		serverError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+tenantCols+` FROM tenants
		WHERE ($1::text IS NULL OR organization_id = $1) AND ($2::text IS NULL OR status = $2)
		ORDER BY created_at ASC, id ASC
		LIMIT $3 OFFSET $4`, org, status, perPage, (page-1)*perPage)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.Tenant, 0)
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, models.PaginatedResponse[models.Tenant]{Data: list, Total: total, Page: page, PerPage: perPage})
}

func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	t, err := scanTenant(h.db.QueryRowContext(r.Context(),
		`SELECT `+tenantCols+` FROM tenants WHERE id = $1`, chi.URLParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.CanAccessOrg(t.OrganizationID)) {
		notFound(w, "Tenant")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var req models.CreateTenantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DatabaseMode != "" && req.DatabaseMode != "existing" && req.DatabaseMode != "managed" {
		respondError(w, http.StatusBadRequest, "database_mode must be existing or managed")
		return
	}
	managed := req.DatabaseMode == "managed"
	if managed && !h.cfg.ManagedProvisioning {
		respondError(w, http.StatusServiceUnavailable, "Managed provisioning is not enabled on this deployment")
		return
	}
	name, ok1 := cleanName(req.Name, maxNameLen)
	ext, ok2 := cleanExternalID(req.ExternalTenantID)
	if !ok1 || !ok2 {
		respondError(w, http.StatusBadRequest, "name (max 255) and external_tenant_id (max 64) are required")
		return
	}
	if msg, ok := validateQuota(req.Quota); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	var reqTags *[]string
	if req.Tags != nil {
		reqTags = &req.Tags
	}
	tags, metadata, msg := validateProfile(profileInput{
		Region: req.Region, PlanTier: req.PlanTier, BillingCycle: req.BillingCycle, Tags: reqTags, Metadata: req.Metadata})
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	region, plan, billing := defaultRegion, defaultPlanTier, defaultBillingCycle
	if req.Region != nil {
		region = *req.Region
	}
	if req.PlanTier != nil {
		plan = *req.PlanTier
	}
	if req.BillingCycle != nil {
		billing = *req.BillingCycle
	}
	if tags == nil {
		tags = []string{}
	}
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}
	orgID, ok := resolveCreateOrg(w, p, req.OrganizationID)
	if !ok {
		return
	}

	events, queries := defaultMaxEventsPerDay, defaultMaxConcurrentQueries
	if req.Quota != nil {
		if req.Quota.MaxEventsPerDay != nil {
			events = *req.Quota.MaxEventsPerDay
		}
		if req.Quota.MaxConcurrentQueries != nil {
			queries = *req.Quota.MaxConcurrentQueries
		}
	}

	var t models.Tenant
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		status := "active"
		if managed {
			if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(7243021)`); err != nil {
				return err
			}
			var own, total int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FILTER(WHERE t.organization_id=$1),COUNT(*) FROM tenant_provisioning j JOIN tenants t ON t.id=j.tenant_id`, orgID).Scan(&own, &total); err != nil {
				return err
			}
			if own >= 5 || total >= 20 {
				return errProvisionLimit
			}
			status = "provisioning"
		}
		var err error
		t, err = scanTenant(tx.QueryRowContext(r.Context(), `
			INSERT INTO tenants (id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries,
				region, plan_tier, billing_cycle, tags, metadata)
			VALUES ($1, $2, $3, $4, $12, $5, $6, $7, $8, $9, $10, $11::jsonb)
			RETURNING `+tenantCols, newID("tenant_"), orgID, ext, name, events, queries,
			region, plan, billing, pq.Array(tags), string(metadata), status))
		if err != nil {
			return err
		}
		if managed {
			if _, err := tx.ExecContext(r.Context(), `INSERT INTO tenant_provisioning(tenant_id) VALUES($1)`, t.ID); err != nil {
				return err
			}
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: "tenant.create", ResourceType: "tenant", ResourceID: t.ID,
			Details: map[string]any{"name": name, "external_tenant_id": ext, "quota": t.Quota,
				"region": t.Region, "plan_tier": t.PlanTier, "billing_cycle": t.BillingCycle}})
	})
	if errors.Is(err, errProvisionLimit) {
		respondError(w, http.StatusConflict, "Managed database limit reached (5 per organization, 20 per deployment)")
		return
	}
	if err != nil {
		if isForeignKeyViolation(err) {
			notFound(w, "Organization")
			return
		}
		writeDBError(w, r, err, "A tenant with this external_tenant_id already exists in the organization")
		return
	}
	if managed {
		respondJSON(w, http.StatusAccepted, t)
	} else {
		respondJSON(w, http.StatusCreated, t)
	}
}

func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req models.UpdateTenantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Status == nil && req.Quota == nil && req.Region == nil && req.PlanTier == nil &&
		req.BillingCycle == nil && req.Tags == nil && len(bytes.TrimSpace(req.Metadata)) == 0 {
		respondError(w, http.StatusBadRequest, "Nothing to update: provide at least one field")
		return
	}
	if req.Status != nil && !tenantStatuses[*req.Status] {
		respondError(w, http.StatusBadRequest, "status must be one of active, suspended, inactive")
		return
	}
	var name *string
	if req.Name != nil {
		n, ok := cleanName(*req.Name, maxNameLen)
		if !ok {
			respondError(w, http.StatusBadRequest, "name must be 1-255 characters")
			return
		}
		name = &n
	}
	if msg, ok := validateQuota(req.Quota); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	var events *int64
	var queries *int
	if req.Quota != nil {
		events, queries = req.Quota.MaxEventsPerDay, req.Quota.MaxConcurrentQueries
	}
	tags, metadata, msg := validateProfile(profileInput{
		Region: req.Region, PlanTier: req.PlanTier, BillingCycle: req.BillingCycle, Tags: req.Tags, Metadata: req.Metadata})
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	// A nil tags or metadata argument leaves the stored value unchanged.
	var tagsArg, metadataArg interface{}
	if req.Tags != nil {
		tagsArg = pq.Array(tags)
	}
	if metadata != nil {
		metadataArg = string(metadata)
	}

	var t models.Tenant
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		before, err := scanTenant(tx.QueryRowContext(r.Context(),
			`SELECT `+tenantCols+` FROM tenants WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if !p.CanAccessOrg(before.OrganizationID) {
			return sql.ErrNoRows
		}

		if req.Status != nil {
			var busy bool
			if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_provisioning WHERE tenant_id=$1 AND status<>'ready')`, id).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return errManagedLifecycle
			}
		}

		// All requested changes are applied in one statement, so a partial
		// update can never be observed or half-committed.
		t, err = scanTenant(tx.QueryRowContext(r.Context(), `
			UPDATE tenants SET
				name = COALESCE($2, name),
				status = COALESCE($3, status),
				max_events_per_day = COALESCE($4, max_events_per_day),
				max_concurrent_queries = COALESCE($5, max_concurrent_queries),
				region = COALESCE($6, region),
				plan_tier = COALESCE($7, plan_tier),
				billing_cycle = COALESCE($8, billing_cycle),
				tags = COALESCE($9::text[], tags),
				metadata = COALESCE($10::jsonb, metadata)
			WHERE id = $1
			RETURNING `+tenantCols, id, name, req.Status, events, queries,
			req.Region, req.PlanTier, req.BillingCycle, tagsArg, metadataArg))
		if err != nil {
			return err
		}

		changes := map[string]any{}
		if name != nil && *name != before.Name {
			changes["name"] = map[string]any{"from": before.Name, "to": t.Name}
		}
		if req.Status != nil && *req.Status != before.Status {
			changes["status"] = map[string]any{"from": before.Status, "to": t.Status}
		}
		if before.Quota.MaxEventsPerDay != t.Quota.MaxEventsPerDay {
			changes["max_events_per_day"] = map[string]any{"from": before.Quota.MaxEventsPerDay, "to": t.Quota.MaxEventsPerDay}
		}
		if before.Quota.MaxConcurrentQueries != t.Quota.MaxConcurrentQueries {
			changes["max_concurrent_queries"] = map[string]any{"from": before.Quota.MaxConcurrentQueries, "to": t.Quota.MaxConcurrentQueries}
		}
		if before.Region != t.Region {
			changes["region"] = map[string]any{"from": before.Region, "to": t.Region}
		}
		if before.PlanTier != t.PlanTier {
			changes["plan_tier"] = map[string]any{"from": before.PlanTier, "to": t.PlanTier}
		}
		if before.BillingCycle != t.BillingCycle {
			changes["billing_cycle"] = map[string]any{"from": before.BillingCycle, "to": t.BillingCycle}
		}
		if strings.Join(before.Tags, ",") != strings.Join(t.Tags, ",") {
			changes["tags"] = map[string]any{"from": before.Tags, "to": t.Tags}
		}
		if !bytes.Equal(canonicalJSON(before.Metadata), canonicalJSON(t.Metadata)) {
			changes["metadata"] = "changed" // contents omitted: it is free-form and may be large
		}
		if len(changes) == 0 {
			return nil // nothing actually changed; do not write a misleading audit record
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: t.OrganizationID, Action: "tenant.update",
			ResourceType: "tenant", ResourceID: id, Details: changes})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return
	}
	if errors.Is(err, errManagedLifecycle) {
		respondError(w, http.StatusConflict, "Wait for managed provisioning to finish or retry the failed job")
		return
	}
	if err != nil {
		writeDBError(w, r, err, "Tenant already exists")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// DeleteTenant removes a tenant together with its mappings and policies.
// Already-ingested analytics rows are keyed by tenant id and are not touched here.
func (h *Handler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		t, err := scanTenant(tx.QueryRowContext(r.Context(),
			`SELECT `+tenantCols+` FROM tenants WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if !p.CanAccessOrg(t.OrganizationID) {
			return sql.ErrNoRows
		}
		var managed bool
		if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_provisioning WHERE tenant_id=$1)`, id).Scan(&managed); err != nil {
			return err
		}
		if managed {
			return errManagedLifecycle
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM tenants WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: t.OrganizationID, Action: "tenant.delete", ResourceType: "tenant",
			ResourceID: id, Details: map[string]any{"name": t.Name, "external_tenant_id": t.ExternalTenantID}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return
	}
	if errors.Is(err, errManagedLifecycle) {
		respondError(w, http.StatusConflict, "Managed databases require operator backup and decommissioning; ordinary tenant deletion is disabled")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
