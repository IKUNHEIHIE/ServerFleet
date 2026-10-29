package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists gateway state. It never authorizes callers; the Service layer
// enforces permission, task submission and audit.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("gateway: database is required")
	}
	return &Repo{db: db}, nil
}

const gatewaySelect = `SELECT id, node_id, name, region, country_code, status,
	nginx_installed, checks_passed, enabled, last_error, nginx_version, revision,
	created_at, updated_at FROM gateway_nodes`

func (r *Repo) CreateGateway(ctx context.Context, q storage.Queryer, gateway GatewayNode) (GatewayNode, error) {
	if r == nil || q == nil {
		return GatewayNode{}, errors.New("gateway: repository is not configured")
	}
	if err := validateGatewayRecord(gateway); err != nil {
		return GatewayNode{}, err
	}
	_, err := q.Exec(ctx, `INSERT INTO gateway_nodes
		(id, node_id, name, region, country_code, status, nginx_installed, checks_passed,
		 enabled, last_error, nginx_version, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gateway.ID, gateway.NodeID, gateway.Name, gateway.Region, gateway.CountryCode,
		string(gateway.Status), boolInt(gateway.NginxInstalled), boolInt(gateway.ChecksPassed),
		boolInt(gateway.Enabled), gateway.LastError, gateway.NginxVersion, gateway.Revision,
		storage.EncodeTime(gateway.CreatedAt), storage.EncodeTime(gateway.UpdatedAt))
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return GatewayNode{}, ErrGatewayExists
		}
		return GatewayNode{}, fmt.Errorf("gateway: insert gateway: %w", err)
	}
	return gateway, nil
}

func (r *Repo) GetGateway(ctx context.Context, q storage.Queryer, id string) (GatewayNode, error) {
	if r == nil || q == nil {
		return GatewayNode{}, errors.New("gateway: repository is not configured")
	}
	gateway, err := scanGateway(q.QueryRow(ctx, gatewaySelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return GatewayNode{}, ErrGatewayNotFound
	}
	if err != nil {
		return GatewayNode{}, err
	}
	return gateway, nil
}

func (r *Repo) ListGateways(ctx context.Context, q storage.Queryer) ([]GatewayNode, error) {
	if r == nil || q == nil {
		return nil, errors.New("gateway: repository is not configured")
	}
	rows, err := q.Query(ctx, gatewaySelect+" ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gateways := make([]GatewayNode, 0)
	for rows.Next() {
		gateway, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		gateways = append(gateways, gateway)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return gateways, nil
}

// UpdateGateway changes only operator metadata and admission intent. Status and
// check flags remain owned by provisioning/check reconciliation.
func (r *Repo) UpdateGateway(ctx context.Context, q storage.Queryer, gateway GatewayNode, expectedRevision uint64) (GatewayNode, error) {
	if expectedRevision == 0 {
		return GatewayNode{}, ErrInvalidRevision
	}
	current, err := r.GetGateway(ctx, q, gateway.ID)
	if err != nil {
		return GatewayNode{}, err
	}
	if current.Revision != expectedRevision {
		return GatewayNode{}, ErrRevisionConflict
	}
	if err := validateGatewayRecord(gateway); err != nil {
		return GatewayNode{}, err
	}
	outcome, err := q.Exec(ctx, `UPDATE gateway_nodes SET name = ?, region = ?, country_code = ?,
		enabled = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
		gateway.Name, gateway.Region, gateway.CountryCode, boolInt(gateway.Enabled),
		storage.EncodeTime(gateway.UpdatedAt), gateway.ID, expectedRevision)
	if err != nil {
		return GatewayNode{}, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return GatewayNode{}, err
	}
	if affected != 1 {
		return GatewayNode{}, ErrRevisionConflict
	}
	gateway.Revision = expectedRevision + 1
	gateway.CreatedAt = current.CreatedAt
	return gateway, nil
}

// UpdateGatewayState replaces the lifecycle fields computed by provisioning or a
// check report. It is revision checked like every other mutation.
func (r *Repo) UpdateGatewayState(ctx context.Context, q storage.Queryer, gateway GatewayNode, expectedRevision uint64) (GatewayNode, error) {
	if expectedRevision == 0 {
		return GatewayNode{}, ErrInvalidRevision
	}
	current, err := r.GetGateway(ctx, q, gateway.ID)
	if err != nil {
		return GatewayNode{}, err
	}
	if current.Revision != expectedRevision {
		return GatewayNode{}, ErrRevisionConflict
	}
	if err := validateGatewayRecord(gateway); err != nil {
		return GatewayNode{}, err
	}
	outcome, err := q.Exec(ctx, `UPDATE gateway_nodes SET status = ?, nginx_installed = ?,
		checks_passed = ?, enabled = ?, last_error = ?, nginx_version = ?,
		revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
		string(gateway.Status), boolInt(gateway.NginxInstalled), boolInt(gateway.ChecksPassed),
		boolInt(gateway.Enabled), gateway.LastError, gateway.NginxVersion,
		storage.EncodeTime(gateway.UpdatedAt), gateway.ID, expectedRevision)
	if err != nil {
		return GatewayNode{}, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return GatewayNode{}, err
	}
	if affected != 1 {
		return GatewayNode{}, ErrRevisionConflict
	}
	gateway.Revision = expectedRevision + 1
	gateway.CreatedAt = current.CreatedAt
	return gateway, nil
}

const routeSelect = `SELECT id, gateway_id, domain, upstream, service_group_id,
	service_group_revision, https, enabled, revision, created_at, updated_at
	FROM gateway_routes`

func (r *Repo) CreateRoute(ctx context.Context, q storage.Queryer, route GatewayRoute) (GatewayRoute, error) {
	if r == nil || q == nil {
		return GatewayRoute{}, errors.New("gateway: repository is not configured")
	}
	if err := validateRouteRecord(route); err != nil {
		return GatewayRoute{}, err
	}
	_, err := q.Exec(ctx, `INSERT INTO gateway_routes
		(id, gateway_id, domain, upstream, service_group_id, service_group_revision,
		 https, enabled, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		route.ID, route.GatewayID, route.Domain, route.Upstream, route.ServiceGroupID,
		int64(route.ServiceGroupRevision), boolInt(route.HTTPS), boolInt(route.Enabled),
		route.Revision, storage.EncodeTime(route.CreatedAt), storage.EncodeTime(route.UpdatedAt))
	if err != nil {
		if storage.IsUniqueViolation(err) {
			var existing int
			if queryErr := q.QueryRow(ctx, "SELECT COUNT(*) FROM gateway_routes WHERE id = ?", route.ID).Scan(&existing); queryErr == nil && existing > 0 {
				return GatewayRoute{}, ErrRouteExists
			}
			return GatewayRoute{}, ErrRouteDomainTaken
		}
		return GatewayRoute{}, fmt.Errorf("gateway: insert route: %w", err)
	}
	return route, nil
}

func (r *Repo) GetRoute(ctx context.Context, q storage.Queryer, id string) (GatewayRoute, error) {
	if r == nil || q == nil {
		return GatewayRoute{}, errors.New("gateway: repository is not configured")
	}
	route, err := scanRoute(q.QueryRow(ctx, routeSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return GatewayRoute{}, ErrRouteNotFound
	}
	if err != nil {
		return GatewayRoute{}, err
	}
	return route, nil
}

func (r *Repo) ListRoutes(ctx context.Context, q storage.Queryer, gatewayID string) ([]GatewayRoute, error) {
	if r == nil || q == nil {
		return nil, errors.New("gateway: repository is not configured")
	}
	rows, err := q.Query(ctx, routeSelect+" WHERE gateway_id = ? ORDER BY domain, id", gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := make([]GatewayRoute, 0)
	for rows.Next() {
		route, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return routes, nil
}

func (r *Repo) UpdateRoute(ctx context.Context, q storage.Queryer, route GatewayRoute, expectedRevision uint64) (GatewayRoute, error) {
	if expectedRevision == 0 {
		return GatewayRoute{}, ErrInvalidRevision
	}
	current, err := r.GetRoute(ctx, q, route.ID)
	if err != nil {
		return GatewayRoute{}, err
	}
	if current.GatewayID != route.GatewayID {
		return GatewayRoute{}, ErrRouteNotFound
	}
	if current.Revision != expectedRevision {
		return GatewayRoute{}, ErrRevisionConflict
	}
	if err := validateRouteRecord(route); err != nil {
		return GatewayRoute{}, err
	}
	outcome, err := q.Exec(ctx, `UPDATE gateway_routes SET domain = ?, upstream = ?,
		service_group_id = ?, service_group_revision = ?, https = ?, enabled = ?,
		revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
		route.Domain, route.Upstream, route.ServiceGroupID, int64(route.ServiceGroupRevision),
		boolInt(route.HTTPS), boolInt(route.Enabled), storage.EncodeTime(route.UpdatedAt),
		route.ID, expectedRevision)
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return GatewayRoute{}, ErrRouteDomainTaken
		}
		return GatewayRoute{}, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return GatewayRoute{}, err
	}
	if affected != 1 {
		return GatewayRoute{}, ErrRevisionConflict
	}
	route.Revision = expectedRevision + 1
	route.CreatedAt = current.CreatedAt
	return route, nil
}

func (r *Repo) DeleteRoute(ctx context.Context, q storage.Queryer, gatewayID, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	current, err := r.GetRoute(ctx, q, id)
	if err != nil {
		return err
	}
	if current.GatewayID != gatewayID {
		return ErrRouteNotFound
	}
	if current.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	outcome, err := q.Exec(ctx, "DELETE FROM gateway_routes WHERE id = ? AND revision = ?", id, expectedRevision)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRevisionConflict
	}
	return nil
}

const provisionSelect = `SELECT id, gateway_id, task_id, status, steps, error,
	created_at, updated_at, started_at, finished_at FROM gateway_provisions`

func (r *Repo) CreateProvision(ctx context.Context, q storage.Queryer, provision Provision) (Provision, error) {
	if r == nil || q == nil {
		return Provision{}, errors.New("gateway: repository is not configured")
	}
	if err := validateProvisionRecord(provision); err != nil {
		return Provision{}, err
	}
	steps, err := json.Marshal(provision.Steps)
	if err != nil {
		return Provision{}, fmt.Errorf("gateway: encode provision steps: %w", err)
	}
	if provision.Steps == nil {
		steps = []byte("[]")
	}
	_, err = q.Exec(ctx, `INSERT INTO gateway_provisions
		(id, gateway_id, task_id, status, steps, error, created_at, updated_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provision.ID, provision.GatewayID, provision.TaskID, string(provision.Status),
		string(steps), provision.Error, storage.EncodeTime(provision.CreatedAt),
		storage.EncodeTime(provision.UpdatedAt), optionalTime(provision.StartedAt),
		optionalTime(provision.FinishedAt))
	if err != nil {
		if storage.IsUniqueViolation(err) {
			if _, lookupErr := r.GetProvisionByTaskID(ctx, q, provision.TaskID); lookupErr == nil {
				return Provision{}, ErrProvisionExists
			}
			if active, activeErr := r.HasActiveProvision(ctx, q, provision.GatewayID); activeErr == nil && active {
				return Provision{}, ErrProvisionInProgress
			}
			return Provision{}, ErrProvisionExists
		}
		return Provision{}, fmt.Errorf("gateway: insert provision: %w", err)
	}
	return provision, nil
}

func (r *Repo) GetProvision(ctx context.Context, q storage.Queryer, id string) (Provision, error) {
	if r == nil || q == nil {
		return Provision{}, errors.New("gateway: repository is not configured")
	}
	provision, err := scanProvision(q.QueryRow(ctx, provisionSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Provision{}, ErrProvisionNotFound
	}
	return provision, err
}

func (r *Repo) GetProvisionByTaskID(ctx context.Context, q storage.Queryer, taskID string) (Provision, error) {
	if r == nil || q == nil {
		return Provision{}, errors.New("gateway: repository is not configured")
	}
	provision, err := scanProvision(q.QueryRow(ctx, provisionSelect+" WHERE task_id = ?", taskID))
	if storage.IsNotFound(err) {
		return Provision{}, ErrProvisionNotFound
	}
	return provision, err
}

func (r *Repo) ListProvisions(ctx context.Context, q storage.Queryer, gatewayID string) ([]Provision, error) {
	if r == nil || q == nil {
		return nil, errors.New("gateway: repository is not configured")
	}
	rows, err := q.Query(ctx, provisionSelect+" WHERE gateway_id = ? ORDER BY created_at, id", gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	provisions := make([]Provision, 0)
	for rows.Next() {
		provision, err := scanProvision(rows)
		if err != nil {
			return nil, err
		}
		provisions = append(provisions, provision)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return provisions, nil
}

func (r *Repo) HasActiveProvision(ctx context.Context, q storage.Queryer, gatewayID string) (bool, error) {
	if r == nil || q == nil {
		return false, errors.New("gateway: repository is not configured")
	}
	var count int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM gateway_provisions
		WHERE gateway_id = ? AND status IN ('queued','running')`, gatewayID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateProvision applies one transition guarded by the previous status, so a
// concurrent reconciler cannot silently overwrite a terminal result.
func (r *Repo) UpdateProvision(ctx context.Context, q storage.Queryer, provision Provision, expected ProvisionStatus) (Provision, error) {
	if r == nil || q == nil {
		return Provision{}, errors.New("gateway: repository is not configured")
	}
	if !expected.Valid() {
		return Provision{}, fmt.Errorf("%w: previous status", ErrInvalidProvision)
	}
	if err := validateProvisionRecord(provision); err != nil {
		return Provision{}, err
	}
	steps, err := json.Marshal(provision.Steps)
	if err != nil {
		return Provision{}, fmt.Errorf("gateway: encode provision steps: %w", err)
	}
	if provision.Steps == nil {
		steps = []byte("[]")
	}
	outcome, err := q.Exec(ctx, `UPDATE gateway_provisions SET status = ?, steps = ?, error = ?,
		updated_at = ?, started_at = ?, finished_at = ? WHERE id = ? AND status = ?`,
		string(provision.Status), string(steps), provision.Error, storage.EncodeTime(provision.UpdatedAt),
		optionalTime(provision.StartedAt), optionalTime(provision.FinishedAt),
		provision.ID, string(expected))
	if err != nil {
		return Provision{}, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return Provision{}, err
	}
	if affected != 1 {
		return Provision{}, ErrProvisionConflict
	}
	return provision, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanGateway(row scanner) (GatewayNode, error) {
	var (
		gateway                         GatewayNode
		status                          string
		nginxInstalled, checks, enabled int
		revision                        int64
		created, updated                string
	)
	if err := row.Scan(&gateway.ID, &gateway.NodeID, &gateway.Name, &gateway.Region,
		&gateway.CountryCode, &status, &nginxInstalled, &checks, &enabled, &gateway.LastError,
		&gateway.NginxVersion, &revision, &created, &updated); err != nil {
		return GatewayNode{}, err
	}
	gateway.Status = Status(status)
	if !gateway.Status.Valid() {
		return GatewayNode{}, fmt.Errorf("gateway: stored gateway %q has invalid status", gateway.ID)
	}
	if revision < 1 {
		return GatewayNode{}, fmt.Errorf("gateway: stored gateway %q has invalid revision", gateway.ID)
	}
	gateway.Revision = uint64(revision)
	gateway.NginxInstalled = nginxInstalled != 0
	gateway.ChecksPassed = checks != 0
	gateway.Enabled = enabled != 0
	var err error
	if gateway.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return GatewayNode{}, err
	}
	if gateway.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return GatewayNode{}, err
	}
	if err := validateGatewayRecord(gateway); err != nil {
		return GatewayNode{}, err
	}
	return gateway, nil
}

func scanRoute(row scanner) (GatewayRoute, error) {
	var (
		route            GatewayRoute
		revision         int64
		groupRevision    int64
		https, enabled   int
		created, updated string
	)
	if err := row.Scan(&route.ID, &route.GatewayID, &route.Domain, &route.Upstream,
		&route.ServiceGroupID, &groupRevision, &https, &enabled, &revision,
		&created, &updated); err != nil {
		return GatewayRoute{}, err
	}
	if revision < 1 || groupRevision < 0 {
		return GatewayRoute{}, fmt.Errorf("gateway: stored route %q has invalid revision", route.ID)
	}
	route.Revision = uint64(revision)
	route.ServiceGroupRevision = uint64(groupRevision)
	route.HTTPS = https != 0
	route.Enabled = enabled != 0
	var err error
	if route.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return GatewayRoute{}, err
	}
	if route.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return GatewayRoute{}, err
	}
	if err := validateRouteRecord(route); err != nil {
		return GatewayRoute{}, err
	}
	return route, nil
}

func scanProvision(row scanner) (Provision, error) {
	var (
		provision         Provision
		status            string
		steps             string
		created, updated  string
		started, finished storage.NullTime
	)
	if err := row.Scan(&provision.ID, &provision.GatewayID, &provision.TaskID, &status,
		&steps, &provision.Error, &created, &updated, &started, &finished); err != nil {
		return Provision{}, err
	}
	provision.Status = ProvisionStatus(status)
	if !provision.Status.Valid() {
		return Provision{}, fmt.Errorf("gateway: stored provision %q has invalid status", provision.ID)
	}
	if steps != "" && steps != "null" {
		if err := json.Unmarshal([]byte(steps), &provision.Steps); err != nil {
			return Provision{}, fmt.Errorf("gateway: decode provision steps: %w", err)
		}
	}
	if provision.Steps == nil {
		provision.Steps = []ProvisionStep{}
	}
	if err := validateStoredSteps(provision.Steps); err != nil {
		return Provision{}, err
	}
	var err error
	if provision.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Provision{}, err
	}
	if provision.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Provision{}, err
	}
	if started.Valid {
		provision.StartedAt = started.Time
	}
	if finished.Valid {
		provision.FinishedAt = finished.Time
	}
	if err := validateProvisionRecord(provision); err != nil {
		return Provision{}, err
	}
	return provision, nil
}

func validateGatewayRecord(gateway GatewayNode) error {
	if !validID(gateway.ID) {
		return fmt.Errorf("%w: id", ErrInvalidGateway)
	}
	if !validID(gateway.NodeID) {
		return fmt.Errorf("%w: node id", ErrInvalidGateway)
	}
	if !validText(gateway.Name, maxGatewayNameBytes) {
		return fmt.Errorf("%w: name", ErrInvalidGateway)
	}
	if gateway.Region != "" && !validText(gateway.Region, maxRegionBytes) {
		return fmt.Errorf("%w: region", ErrInvalidGateway)
	}
	if _, err := normalizeCountryCode(gateway.CountryCode); err != nil {
		return err
	}
	if !gateway.Status.Valid() {
		return fmt.Errorf("%w: status", ErrInvalidGateway)
	}
	if gateway.ChecksPassed && !gateway.NginxInstalled {
		return fmt.Errorf("%w: checks passed without nginx", ErrInvalidGateway)
	}
	if gateway.LastError != "" && sanitizeMessage(gateway.LastError) != gateway.LastError {
		return fmt.Errorf("%w: last error is not redacted", ErrInvalidGateway)
	}
	if gateway.NginxVersion != "" && !validText(gateway.NginxVersion, maxVersionBytes) {
		return fmt.Errorf("%w: nginx version", ErrInvalidGateway)
	}
	if gateway.Revision == 0 {
		return fmt.Errorf("%w: revision", ErrInvalidGateway)
	}
	if gateway.CreatedAt.IsZero() || gateway.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: timestamps", ErrInvalidGateway)
	}
	return nil
}

func validateRouteRecord(route GatewayRoute) error {
	if !validID(route.ID) || !validID(route.GatewayID) {
		return fmt.Errorf("%w: id", ErrInvalidRoute)
	}
	if _, err := normalizeDomain(route.Domain); err != nil {
		return err
	}
	if route.Domain != strings.ToLower(route.Domain) {
		return fmt.Errorf("%w: domain must be lowercase", ErrInvalidRoute)
	}
	if (route.Upstream == "") == (route.ServiceGroupID == "") {
		return fmt.Errorf("%w: exactly one route target is required", ErrInvalidRoute)
	}
	if route.Upstream != "" {
		if _, err := normalizeUpstream(route.Upstream); err != nil {
			return err
		}
		if route.ServiceGroupRevision != 0 {
			return fmt.Errorf("%w: upstream route cannot carry a group revision", ErrInvalidRoute)
		}
	} else if !validID(route.ServiceGroupID) || route.ServiceGroupRevision == 0 {
		return fmt.Errorf("%w: service group snapshot", ErrInvalidRoute)
	}
	if route.Revision == 0 {
		return fmt.Errorf("%w: revision", ErrInvalidRoute)
	}
	if route.CreatedAt.IsZero() || route.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: timestamps", ErrInvalidRoute)
	}
	return nil
}

func validateProvisionRecord(provision Provision) error {
	if !validID(provision.ID) || !validID(provision.GatewayID) || !validID(provision.TaskID) {
		return fmt.Errorf("%w: id", ErrInvalidProvision)
	}
	if !provision.Status.Valid() {
		return fmt.Errorf("%w: status", ErrInvalidProvision)
	}
	if provision.Error != "" && sanitizeMessage(provision.Error) != provision.Error {
		return fmt.Errorf("%w: error is not redacted", ErrInvalidProvision)
	}
	if err := validateStoredSteps(provision.Steps); err != nil {
		return err
	}
	if provision.CreatedAt.IsZero() || provision.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: timestamps", ErrInvalidProvision)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func optionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return storage.EncodeTime(value)
}
