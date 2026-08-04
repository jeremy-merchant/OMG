package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

//go:embed migrations/0002_coordination.sql
var coordinationFS embed.FS
var coordinationSQL = mustCoordinationSQL()

//go:embed migrations/0009_handoff_lifecycle.sql
var handoffLifecycleFS embed.FS
var handoffLifecycleSQL = mustHandoffLifecycleSQL()

//go:embed migrations/0010_exact_sha_canary.sql
var exactSHACanaryFS embed.FS
var exactSHACanarySQL = mustExactSHACanarySQL()

//go:embed migrations/0011_automatic_migration_authorization.sql
var automaticMigrationAuthorizationFS embed.FS
var automaticMigrationAuthorizationSQL = mustAutomaticMigrationAuthorizationSQL()

//go:embed migrations/0012_task_hierarchy_policy.sql
var taskHierarchyPolicyFS embed.FS
var taskHierarchyPolicySQL = mustTaskHierarchyPolicySQL()

func mustCoordinationSQL() string {
	b, err := coordinationFS.ReadFile("migrations/0002_coordination.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mustHandoffLifecycleSQL() string {
	b, err := handoffLifecycleFS.ReadFile("migrations/0009_handoff_lifecycle.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mustExactSHACanarySQL() string {
	b, err := exactSHACanaryFS.ReadFile("migrations/0010_exact_sha_canary.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mustAutomaticMigrationAuthorizationSQL() string {
	b, err := automaticMigrationAuthorizationFS.ReadFile("migrations/0011_automatic_migration_authorization.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mustTaskHierarchyPolicySQL() string {
	b, err := taskHierarchyPolicyFS.ReadFile("migrations/0012_task_hierarchy_policy.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (r repositories) Coordination() ports.CoordinationRepository {
	return coordination{tx: r.tx, project: r.project}
}

type coordination struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r coordination) acceptsProject(project string) bool {
	return r.project == "legacy" || project == string(r.project)
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseStamp(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return time.Time{}, e
	}
	return t.UTC(), nil
}
func nullID(v lineage.ID) any {
	if v == "" {
		return nil
	}
	return string(v)
}
func nullTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return stamp(*v)
}
func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func scanTask(row *sql.Row) (lineage.Task, bool, error) {
	var t lineage.Task
	var createdBy, claim, parent, sup sql.NullString
	var created, updated string
	err := row.Scan(&t.ID, &t.ProjectID, &t.DisplayNumber, &t.Title, &t.State, &createdBy, &claim, &parent, &t.CompletionPolicy, &t.ParentRequirement, &created, &updated, &sup)
	if err == sql.ErrNoRows {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	t.CreatedBySessionID = lineage.ID(createdBy.String)
	t.ClaimedBySessionID = lineage.ID(claim.String)
	t.ParentTaskID = lineage.ID(parent.String)
	t.Supersedes = lineage.ID(sup.String)
	if t.CreatedAt, err = parseStamp(created); err != nil {
		return lineage.Task{}, false, err
	}
	if t.UpdatedAt, err = parseStamp(updated); err != nil {
		return lineage.Task{}, false, err
	}
	return t, true, nil
}
func (r coordination) CreateHuman(ctx context.Context, h lineage.Human) error {
	if r.project != "legacy" {
		if h.ProjectID == "" {
			h.ProjectID = lineage.ID(r.project)
		}
		if h.ProjectID != lineage.ID(r.project) {
			return fmt.Errorf("project mismatch")
		}
		if h.Supersedes != "" {
			var valid int
			e := r.tx.QueryRowContext(ctx, "SELECT 1 FROM humans WHERE id=? AND project_id=?", h.Supersedes, r.project).Scan(&valid)
			if e == sql.ErrNoRows {
				return fmt.Errorf("project mismatch")
			}
			if e != nil {
				return e
			}
		}
	} else {
		if h.ProjectID == "" {
			h.ProjectID = "legacy"
		}
		if h.ProjectID != "legacy" {
			return fmt.Errorf("project mismatch")
		}
		if _, e := r.tx.ExecContext(ctx, "INSERT OR IGNORE INTO projects(id,created_at) VALUES(?,?)", h.ProjectID, stamp(h.CreatedAt)); e != nil {
			return e
		}
	}
	if e := h.Validate(); e != nil {
		return e
	}
	_, e := r.tx.ExecContext(ctx, "INSERT INTO humans(id,project_id,display_name,provenance_confidence,created_at,supersedes_id) VALUES(?,?,?,?,?,?)", h.ID, h.ProjectID, h.DisplayName, h.Confidence, stamp(h.CreatedAt), nullID(h.Supersedes))
	return e
}
func (r coordination) GetHuman(ctx context.Context, id lineage.ID) (lineage.Human, bool, error) {
	var h lineage.Human
	var at string
	var sup sql.NullString
	e := r.tx.QueryRowContext(ctx, `SELECT id,COALESCE(project_id,''),display_name,provenance_confidence,created_at,supersedes_id FROM humans WHERE id=? AND (?='legacy' OR project_id=? OR (project_id IS NULL AND EXISTS (SELECT 1 FROM legacy_human_projects WHERE human_id=humans.id AND project_id=?)))`, id, r.project, r.project, r.project).Scan(&h.ID, &h.ProjectID, &h.DisplayName, &h.Confidence, &at, &sup)
	if e == sql.ErrNoRows {
		return h, false, nil
	}
	if e != nil {
		return h, false, e
	}
	if h.CreatedAt, e = parseStamp(at); e != nil {
		return lineage.Human{}, false, e
	}
	h.Supersedes = lineage.ID(sup.String)
	if h.Validate() != nil {
		return lineage.Human{}, false, lineage.ErrInvalid
	}
	return h, true, nil
}
func (r coordination) CreateSession(ctx context.Context, s lineage.AgentSession) error {
	if !r.acceptsProject(string(s.ProjectID)) {
		return fmt.Errorf("project mismatch")
	}
	if r.project != "legacy" {
		var valid int
		e := r.tx.QueryRowContext(ctx, `SELECT 1 WHERE
			(? IS NULL OR EXISTS (SELECT 1 FROM humans h WHERE h.id=? AND (h.project_id=? OR (h.project_id IS NULL AND EXISTS (SELECT 1 FROM legacy_human_projects a WHERE a.human_id=h.id AND a.project_id=?))))) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id<>?)) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id<>?)) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id<>?)) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id<>?))`,
			nullID(s.HumanID), s.HumanID, s.ProjectID, s.ProjectID,
			nullID(s.ParentID), s.ParentID, s.ProjectID,
			nullID(s.RootID), s.RootID, s.ProjectID,
			nullID(s.ContinuationOfID), s.ContinuationOfID, s.ProjectID,
			nullID(s.TaskID), s.TaskID, s.ProjectID,
		).Scan(&valid)
		if e == sql.ErrNoRows {
			return fmt.Errorf("project mismatch")
		}
		if e != nil {
			return e
		}
	}
	accessState := s.NativeAccessState
	if accessState == "" {
		accessState = lineage.NativeAccessUnsupported
	}
	_, e := r.tx.ExecContext(ctx, "INSERT INTO agent_sessions(id,project_id,human_id,lineage_kind,runtime,role,instruction_source,source_ref,parent_session_id,root_session_id,continuation_of_id,task_id,worktree_ref,native_access_state,runtime_home,native_session_id,native_session_ref,native_session_started_at,native_session_fingerprint,native_parent_session_id,started_at,ended_at,interrupted_at,heartbeat_at,supersedes_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", s.ID, s.ProjectID, nullID(s.HumanID), s.Kind, s.Runtime, s.Role, s.Source, s.SourceRef, nullID(s.ParentID), nullID(s.RootID), nullID(s.ContinuationOfID), nullID(s.TaskID), s.WorktreeRef, accessState, nullString(s.RuntimeHome), nullString(s.NativeSessionID), nullString(s.NativeSessionRef), nullTime(s.NativeSessionStartedAt), nullString(s.NativeSessionFingerprint), nullString(s.NativeParentSessionID), stamp(s.StartedAt), nullTime(s.EndedAt), nullTime(s.InterruptedAt), nullTime(s.HeartbeatAt), nullID(s.Supersedes))
	return e
}
func (r coordination) GetSession(ctx context.Context, id lineage.ID) (lineage.AgentSession, bool, error) {
	var s lineage.AgentSession
	var human, parent, root, cont, task, runtimeHome, nativeID, nativeRef, nativeStarted, nativeFingerprint, nativeParent, ended, intr, hb, live, sup sql.NullString
	var at string
	e := r.tx.QueryRowContext(ctx, `SELECT id,project_id,human_id,lineage_kind,runtime,role,instruction_source,source_ref,parent_session_id,root_session_id,continuation_of_id,task_id,worktree_ref,native_access_state,runtime_home,native_session_id,native_session_ref,native_session_started_at,native_session_fingerprint,native_parent_session_id,started_at,ended_at,
		CASE WHEN interrupted_at IS NOT NULL OR EXISTS (SELECT 1 FROM session_heartbeats h WHERE h.session_id=agent_sessions.id AND h.liveness='interrupted') THEN COALESCE((SELECT h.observed_at FROM session_heartbeats h WHERE h.session_id=agent_sessions.id AND h.liveness='interrupted' ORDER BY h.observed_at DESC,h.id DESC LIMIT 1), interrupted_at) END,
		COALESCE((SELECT h.observed_at FROM session_heartbeats h WHERE h.session_id=agent_sessions.id ORDER BY h.observed_at DESC,h.id DESC LIMIT 1),heartbeat_at),
		CASE WHEN interrupted_at IS NOT NULL OR EXISTS (SELECT 1 FROM session_heartbeats h WHERE h.session_id=agent_sessions.id AND h.liveness='interrupted') THEN 'interrupted' ELSE COALESCE((SELECT h.liveness FROM session_heartbeats h WHERE h.session_id=agent_sessions.id ORDER BY h.observed_at DESC,h.id DESC LIMIT 1),'alive') END,
		supersedes_id FROM agent_sessions WHERE id=? AND (?='legacy' OR project_id=?)`, id, r.project, r.project).Scan(&s.ID, &s.ProjectID, &human, &s.Kind, &s.Runtime, &s.Role, &s.Source, &s.SourceRef, &parent, &root, &cont, &task, &s.WorktreeRef, &s.NativeAccessState, &runtimeHome, &nativeID, &nativeRef, &nativeStarted, &nativeFingerprint, &nativeParent, &at, &ended, &intr, &hb, &live, &sup)
	if e == sql.ErrNoRows {
		return s, false, nil
	}
	if e != nil {
		return s, false, e
	}
	s.HumanID = lineage.ID(human.String)
	s.ParentID = lineage.ID(parent.String)
	s.RootID = lineage.ID(root.String)
	s.ContinuationOfID = lineage.ID(cont.String)
	s.TaskID = lineage.ID(task.String)
	s.RuntimeHome = runtimeHome.String
	s.NativeSessionID = nativeID.String
	s.NativeSessionRef = nativeRef.String
	s.NativeSessionFingerprint = nativeFingerprint.String
	s.NativeParentSessionID = nativeParent.String
	s.Supersedes = lineage.ID(sup.String)
	s.Liveness = lineage.Liveness(live.String)
	if s.StartedAt, e = parseStamp(at); e != nil {
		return lineage.AgentSession{}, false, e
	}
	if nativeStarted.Valid {
		v, e := parseStamp(nativeStarted.String)
		if e != nil {
			return lineage.AgentSession{}, false, e
		}
		s.NativeSessionStartedAt = &v
	}
	if ended.Valid {
		v, e := parseStamp(ended.String)
		if e != nil {
			return lineage.AgentSession{}, false, e
		}
		s.EndedAt = &v
	}
	if intr.Valid {
		v, e := parseStamp(intr.String)
		if e != nil {
			return lineage.AgentSession{}, false, e
		}
		s.InterruptedAt = &v
	}
	if hb.Valid {
		v, e := parseStamp(hb.String)
		if e != nil {
			return lineage.AgentSession{}, false, e
		}
		s.HeartbeatAt = &v
	}
	return s, true, nil
}
func (r coordination) IssueToken(ctx context.Context, t lineage.DelegationToken) error {
	if !r.acceptsProject(string(t.ProjectID)) {
		return fmt.Errorf("project mismatch")
	}
	res, e := r.tx.ExecContext(ctx, "INSERT INTO delegation_tokens(id,project_id,task_id,parent_session_id,algorithm,iterations,salt,verifier,issued_at,expires_at) SELECT ?,?,?,?,?,?,?,?,?,? WHERE EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?) AND (? IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?))", t.ID, t.ProjectID, nullID(t.TaskID), t.ParentSessionID, t.Algorithm, t.Iterations, t.Salt, t.Verifier, stamp(t.IssuedAt), stamp(t.ExpiresAt), t.ParentSessionID, t.ProjectID, nullID(t.TaskID), t.TaskID, t.ProjectID)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("project mismatch")
		}
	}
	return e
}
func (r coordination) RevokeToken(ctx context.Context, id lineage.ID, at time.Time) error {
	res, e := r.tx.ExecContext(ctx, "UPDATE delegation_tokens SET revoked_at=? WHERE id=? AND (?='legacy' OR project_id=?) AND revoked_at IS NULL AND consumed_at IS NULL", stamp(at), id, r.project, r.project)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("token unavailable")
		}
	}
	return e
}
func (r coordination) FindTokenByVerifier(ctx context.Context, p, t, par lineage.ID) ([]lineage.DelegationToken, error) {
	if !r.acceptsProject(string(p)) {
		return nil, nil
	}
	rows, e := r.tx.QueryContext(ctx, "SELECT id,project_id,task_id,parent_session_id,algorithm,iterations,salt,verifier,issued_at,expires_at,revoked_at,consumed_at,consumed_by_session_id FROM delegation_tokens WHERE project_id=? AND (task_id IS ? OR task_id=?) AND parent_session_id=?", p, nullID(t), t, par)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []lineage.DelegationToken
	for rows.Next() {
		var x lineage.DelegationToken
		var task, rev, con, by sql.NullString
		var issued, expires string
		if e = rows.Scan(&x.ID, &x.ProjectID, &task, &x.ParentSessionID, &x.Algorithm, &x.Iterations, &x.Salt, &x.Verifier, &issued, &expires, &rev, &con, &by); e != nil {
			return nil, e
		}
		x.TaskID = lineage.ID(task.String)
		if x.IssuedAt, e = parseStamp(issued); e != nil {
			return nil, e
		}
		if x.ExpiresAt, e = parseStamp(expires); e != nil {
			return nil, e
		}
		if rev.Valid {
			at, e := parseStamp(rev.String)
			if e != nil {
				return nil, e
			}
			x.RevokedAt = &at
		}
		if con.Valid {
			at, e := parseStamp(con.String)
			if e != nil {
				return nil, e
			}
			x.ConsumedAt = &at
		}
		x.ConsumedBySessionID = lineage.ID(by.String)
		out = append(out, x)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
func (r coordination) GetToken(ctx context.Context, id lineage.ID) (lineage.DelegationToken, bool, error) {
	var x lineage.DelegationToken
	var task, rev, con, by sql.NullString
	var issued, expires string
	e := r.tx.QueryRowContext(ctx, "SELECT id,project_id,task_id,parent_session_id,algorithm,iterations,salt,verifier,issued_at,expires_at,revoked_at,consumed_at,consumed_by_session_id FROM delegation_tokens WHERE id=? AND (?='legacy' OR project_id=?)", id, r.project, r.project).Scan(&x.ID, &x.ProjectID, &task, &x.ParentSessionID, &x.Algorithm, &x.Iterations, &x.Salt, &x.Verifier, &issued, &expires, &rev, &con, &by)
	if e == sql.ErrNoRows {
		return x, false, nil
	}
	if e != nil {
		return x, false, e
	}
	x.TaskID = lineage.ID(task.String)
	if x.IssuedAt, e = parseStamp(issued); e != nil {
		return lineage.DelegationToken{}, false, e
	}
	if x.ExpiresAt, e = parseStamp(expires); e != nil {
		return lineage.DelegationToken{}, false, e
	}
	if rev.Valid {
		v, e := parseStamp(rev.String)
		if e != nil {
			return lineage.DelegationToken{}, false, e
		}
		x.RevokedAt = &v
	}
	if con.Valid {
		v, e := parseStamp(con.String)
		if e != nil {
			return lineage.DelegationToken{}, false, e
		}
		x.ConsumedAt = &v
	}
	x.ConsumedBySessionID = lineage.ID(by.String)
	return x, true, nil
}
func (r coordination) ConsumeToken(ctx context.Context, id, s lineage.ID, at time.Time) error {
	res, e := r.tx.ExecContext(ctx, "UPDATE delegation_tokens SET consumed_at=?,consumed_by_session_id=? WHERE id=? AND (?='legacy' OR project_id=?) AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>? AND (?='legacy' OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?))", stamp(at), s, id, r.project, r.project, stamp(at), r.project, s, r.project)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("token unavailable")
		}
	}
	return e
}
func (r coordination) CreateTask(ctx context.Context, t lineage.Task) (lineage.Task, error) {
	if !r.acceptsProject(string(t.ProjectID)) {
		return t, fmt.Errorf("project mismatch")
	}
	if r.project != "legacy" {
		var valid int
		e := r.tx.QueryRowContext(ctx, `SELECT 1 WHERE
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id<>?)) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id<>?)) AND
			(? IS NULL OR NOT EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id<>?))`,
			nullID(t.CreatedBySessionID), t.CreatedBySessionID, t.ProjectID,
			nullID(t.ParentTaskID), t.ParentTaskID, t.ProjectID,
			nullID(t.Supersedes), t.Supersedes, t.ProjectID,
		).Scan(&valid)
		if e == sql.ErrNoRows {
			return t, fmt.Errorf("project mismatch")
		}
		if e != nil {
			return t, e
		}
	}
	_, e := r.tx.ExecContext(ctx, "INSERT INTO project_task_sequences(project_id,next_number) VALUES(?,1) ON CONFLICT(project_id) DO NOTHING", t.ProjectID)
	if e != nil {
		return t, e
	}
	var n int64
	e = r.tx.QueryRowContext(ctx, "UPDATE project_task_sequences SET next_number=next_number+1 WHERE project_id=? RETURNING next_number-1", t.ProjectID).Scan(&n)
	if e != nil {
		return t, e
	}
	t.DisplayNumber = n
	_, e = r.tx.ExecContext(ctx, "INSERT INTO tasks(id,project_id,display_number,title,state,created_by_session_id,parent_task_id,completion_policy,parent_requirement,created_at,updated_at,supersedes_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", t.ID, t.ProjectID, t.DisplayNumber, t.Title, t.State, nullID(t.CreatedBySessionID), nullID(t.ParentTaskID), lineage.EffectiveTaskCompletionPolicy(t.CompletionPolicy), lineage.EffectiveTaskParentRequirement(t.ParentRequirement), stamp(t.CreatedAt), stamp(t.UpdatedAt), nullID(t.Supersedes))
	return t, e
}
func (r coordination) ListTasks(ctx context.Context, project domain.ProjectID) ([]lineage.Task, error) {
	if !r.acceptsProject(string(project)) {
		return nil, nil
	}
	rows, e := r.tx.QueryContext(ctx, "SELECT id,project_id,display_number,title,state,created_by_session_id,claimed_by_session_id,parent_task_id,completion_policy,parent_requirement,created_at,updated_at,supersedes_id FROM tasks WHERE project_id=? ORDER BY display_number,id", project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []lineage.Task{}
	for rows.Next() {
		var t lineage.Task
		var createdBy, claim, parent, sup sql.NullString
		var created, updated string
		if e = rows.Scan(&t.ID, &t.ProjectID, &t.DisplayNumber, &t.Title, &t.State, &createdBy, &claim, &parent, &t.CompletionPolicy, &t.ParentRequirement, &created, &updated, &sup); e != nil {
			return nil, e
		}
		t.CreatedBySessionID = lineage.ID(createdBy.String)
		t.ClaimedBySessionID = lineage.ID(claim.String)
		t.ParentTaskID = lineage.ID(parent.String)
		t.Supersedes = lineage.ID(sup.String)
		if t.CreatedAt, e = parseStamp(created); e != nil {
			return nil, e
		}
		if t.UpdatedAt, e = parseStamp(updated); e != nil {
			return nil, e
		}
		if t.ProjectID != lineage.ID(project) || t.Validate() != nil {
			return nil, fmt.Errorf("corrupt task row")
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (r coordination) GetTask(ctx context.Context, id lineage.ID) (lineage.Task, bool, error) {
	return scanTask(r.tx.QueryRowContext(ctx, "SELECT id,project_id,display_number,title,state,created_by_session_id,claimed_by_session_id,parent_task_id,completion_policy,parent_requirement,created_at,updated_at,supersedes_id FROM tasks WHERE id=? AND (?='legacy' OR project_id=?)", id, r.project, r.project))
}
func (r coordination) ClaimTask(ctx context.Context, id, s lineage.ID, at time.Time) (lineage.Task, bool, error) {
	res, e := r.tx.ExecContext(ctx, "UPDATE tasks SET claimed_by_session_id=?,claimed_at=?,state='CLAIMED',updated_at=? WHERE id=? AND (?='legacy' OR project_id=?) AND claimed_by_session_id IS NULL AND state='READY' AND NOT EXISTS (SELECT 1 FROM task_dependencies WHERE blocked_task_id=? AND kind='hard' AND satisfied_at IS NULL) AND (?='legacy' OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?))", s, stamp(at), stamp(at), id, r.project, r.project, id, r.project, s, r.project)
	if e != nil {
		return lineage.Task{}, false, e
	}
	n, _ := res.RowsAffected()
	t, ok, e := r.GetTask(ctx, id)
	return t, ok && n == 1, e
}
func (r coordination) TransitionTask(ctx context.Context, id lineage.ID, state lineage.TaskState, evidence []byte, at time.Time) (lineage.Task, error) {
	t, ok, e := r.GetTask(ctx, id)
	if e != nil || !ok {
		return t, e
	}
	if !lineage.CanTransitionTask(t.State, state, evidence) {
		return t, fmt.Errorf("invalid task transition")
	}
	_, e = r.tx.ExecContext(ctx, "UPDATE tasks SET state=?,updated_at=? WHERE id=? AND (?='legacy' OR project_id=?)", state, stamp(at), id, r.project, r.project)
	t.State = state
	t.UpdatedAt = at.UTC()
	return t, e
}
func (r coordination) CreateRun(ctx context.Context, x lineage.TaskRun) error {
	res, e := r.tx.ExecContext(ctx, "INSERT INTO task_runs(id,task_id,session_id,state,evidence_json,started_at,ended_at,parent_lost_at,supersedes_id) SELECT ?,?,?,?,?,?,?,?,? WHERE (?='legacy' OR (EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?) AND EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?)))", x.ID, x.TaskID, x.SessionID, x.State, x.Evidence, stamp(x.StartedAt), nullTime(x.EndedAt), nullTime(x.ParentLostAt), nullID(x.Supersedes), r.project, x.TaskID, r.project, x.SessionID, r.project)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("project mismatch")
		}
	}
	return e
}
func (r coordination) GetRun(ctx context.Context, id lineage.ID) (lineage.TaskRun, bool, error) {
	var x lineage.TaskRun
	var ended, lost, sup sql.NullString
	var st string
	e := r.tx.QueryRowContext(ctx, "SELECT r.id,r.task_id,r.session_id,r.state,r.evidence_json,r.started_at,r.ended_at,r.parent_lost_at,r.supersedes_id FROM task_runs r JOIN tasks t ON t.id=r.task_id WHERE r.id=? AND (?='legacy' OR t.project_id=?)", id, r.project, r.project).Scan(&x.ID, &x.TaskID, &x.SessionID, &x.State, &x.Evidence, &st, &ended, &lost, &sup)
	if e == sql.ErrNoRows {
		return x, false, nil
	}
	if e != nil {
		return x, false, e
	}
	if x.StartedAt, e = parseStamp(st); e != nil {
		return lineage.TaskRun{}, false, e
	}
	if ended.Valid {
		v, e := parseStamp(ended.String)
		if e != nil {
			return lineage.TaskRun{}, false, e
		}
		x.EndedAt = &v
	}
	if lost.Valid {
		v, e := parseStamp(lost.String)
		if e != nil {
			return lineage.TaskRun{}, false, e
		}
		x.ParentLostAt = &v
	}
	x.Supersedes = lineage.ID(sup.String)
	return x, true, nil
}
func (r coordination) TransitionRun(ctx context.Context, id lineage.ID, state lineage.RunState, evidence []byte, at time.Time) (lineage.TaskRun, error) {
	x, ok, e := r.GetRun(ctx, id)
	if e != nil || !ok {
		return x, e
	}
	if !lineage.CanTransitionRun(x.State, state, evidence) {
		return x, fmt.Errorf("invalid run transition")
	}
	endedAt := nullTime(nil)
	if lineage.RunHasEnded(state) {
		endedAt = nullTime(&at)
	}
	_, e = r.tx.ExecContext(ctx, "UPDATE task_runs SET state=?,evidence_json=?,ended_at=? WHERE id=? AND (?='legacy' OR task_id IN (SELECT id FROM tasks WHERE project_id=?))", state, evidence, endedAt, id, r.project, r.project)
	x.State = state
	x.Evidence = evidence
	if lineage.RunHasEnded(state) {
		ended := at.UTC()
		x.EndedAt = &ended
	} else {
		x.EndedAt = nil
	}
	return x, e
}
func (r coordination) RecordHeartbeat(ctx context.Context, h lineage.Heartbeat) error {
	res, e := r.tx.ExecContext(ctx, `INSERT INTO session_heartbeats(id,session_id,observed_at,liveness,detail_json)
		SELECT ?,?,?,?,?
		WHERE (?='legacy' OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?))
		AND NOT EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND interrupted_at IS NOT NULL)
		AND NOT EXISTS (SELECT 1 FROM session_heartbeats WHERE session_id=? AND liveness='interrupted')`,
		h.ID, h.SessionID, stamp(h.ObservedAt), h.Liveness, h.Detail,
		r.project, h.SessionID, r.project, h.SessionID, h.SessionID)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("session missing or interrupted")
		}
	}
	return e
}

func (r coordination) interruptedHeartbeatAt(ctx context.Context, sessionID lineage.ID) (time.Time, bool, error) {
	var observedAt string
	err := r.tx.QueryRowContext(ctx, `SELECT h.observed_at
		FROM session_heartbeats h
		JOIN agent_sessions s ON s.id=h.session_id
		WHERE h.session_id=? AND h.liveness='interrupted'
		AND (?='legacy' OR s.project_id=?)`,
		sessionID, r.project, r.project).Scan(&observedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	at, err := parseStamp(observedAt)
	if err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}

func (r coordination) RecordParentLoss(ctx context.Context, id lineage.ID, h lineage.Heartbeat) (lineage.TaskRun, error) {
	x, ok, err := r.GetRun(ctx, id)
	if err != nil {
		return x, err
	}
	if !ok {
		return x, fmt.Errorf("run not found")
	}
	if h.SessionID != x.SessionID {
		return x, fmt.Errorf("heartbeat session does not own run")
	}
	lostAt := h.ObservedAt
	if h.Liveness == lineage.Interrupted {
		existing, found, err := r.interruptedHeartbeatAt(ctx, h.SessionID)
		if err != nil {
			return x, err
		}
		if found {
			lostAt = existing
		} else if err := r.RecordHeartbeat(ctx, h); err != nil {
			return x, err
		}
	} else if err := r.RecordHeartbeat(ctx, h); err != nil {
		return x, err
	}
	state := lineage.ParentLossState(h)
	if !lineage.CanTransitionRun(x.State, state, nil) {
		return x, fmt.Errorf("invalid run transition")
	}
	endedAt := nullTime(nil)
	if lineage.RunHasEnded(state) {
		endedAt = nullTime(&lostAt)
	}
	if _, err = r.tx.ExecContext(ctx, "UPDATE task_runs SET state=?,evidence_json=NULL,ended_at=?,parent_lost_at=? WHERE id=? AND (?='legacy' OR task_id IN (SELECT id FROM tasks WHERE project_id=?))", state, endedAt, stamp(lostAt), id, r.project, r.project); err != nil {
		return x, err
	}
	x.State = state
	x.Evidence = nil
	ended := lostAt.UTC()
	x.EndedAt = &ended
	x.ParentLostAt = &ended
	return x, nil
}
