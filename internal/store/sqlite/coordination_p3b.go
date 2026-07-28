package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
)

func encodeJSON(v any) ([]byte, error) { return json.Marshal(v) }
func parseP3BStamp(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid stored timestamp: %w", err)
	}
	return t.UTC(), nil
}
func (r coordination) ListSessions(ctx context.Context, project lineage.ID) ([]lineage.AgentSession, error) {
	if !r.acceptsProject(string(project)) {
		return nil, nil
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT id FROM agent_sessions WHERE project_id=? ORDER BY id", project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lineage.AgentSession{}
	for rows.Next() {
		var id lineage.ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		session, ok, err := r.GetSession(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok || session.ProjectID != project {
			return nil, fmt.Errorf("invalid stored session")
		}
		out = append(out, session)
	}
	return out, rows.Err()
}
func (r coordination) ListRunsForSession(ctx context.Context, project, sessionID lineage.ID) ([]lineage.TaskRun, error) {
	if !r.acceptsProject(string(project)) {
		return nil, nil
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT r.id FROM task_runs r JOIN tasks t ON t.id=r.task_id JOIN agent_sessions s ON s.id=r.session_id WHERE r.session_id=? AND t.project_id=? AND s.project_id=? ORDER BY r.started_at,r.id", sessionID, project, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lineage.TaskRun{}
	for rows.Next() {
		var id lineage.ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		run, ok, err := r.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok || run.SessionID != sessionID {
			return nil, fmt.Errorf("invalid stored run")
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
func targetColumns(t coord.RecipientTarget) (any, any, any, any) {
	var s, h, task, role any
	if t.SessionID != "" {
		s = t.SessionID
	}
	if t.HumanID != "" {
		h = t.HumanID
	}
	if t.TaskID != "" {
		task = t.TaskID
	}
	if t.Role != "" {
		role = t.Role
	}
	return s, h, task, role
}
func scanProgress(rows *sql.Rows) ([]coord.Progress, error) {
	out := []coord.Progress{}
	for rows.Next() {
		var p coord.Progress
		var done, doing, next []byte
		var run, sup sql.NullString
		var at string
		if err := rows.Scan(&p.ID, &p.TaskID, &run, &p.SessionID, &p.Phase, &done, &doing, &next, &at, &sup); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(done, &p.Done); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(doing, &p.Doing); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(next, &p.Next); err != nil {
			return nil, err
		}
		var err error
		p.CreatedAt, err = parseP3BStamp(at)
		if err != nil {
			return nil, err
		}
		p.RunID = run.String
		p.SupersedesID = sup.String
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r coordination) CreateProgress(ctx context.Context, p coord.Progress) error {
	d, e := encodeJSON(p.Done)
	if e != nil {
		return e
	}
	doing, e := encodeJSON(p.Doing)
	if e != nil {
		return e
	}
	next, e := encodeJSON(p.Next)
	if e != nil {
		return e
	}
	res, e := r.tx.ExecContext(ctx, "INSERT INTO progress_updates(id,task_id,run_id,session_id,phase,done_json,doing_json,next_json,created_at,supersedes_id) SELECT ?,?,?,?,?,?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?) AND EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?) AND (? IS NULL OR EXISTS (SELECT 1 FROM task_runs r JOIN tasks t ON t.id=r.task_id JOIN agent_sessions s ON s.id=r.session_id WHERE r.id=? AND r.task_id=? AND r.session_id=? AND t.project_id=? AND s.project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM progress_updates p JOIN tasks t ON t.id=p.task_id WHERE p.id=? AND t.project_id=?)))", p.ID, p.TaskID, nullString(p.RunID), p.SessionID, p.Phase, d, doing, next, stamp(p.CreatedAt), nullString(p.SupersedesID), r.project, p.TaskID, r.project, p.SessionID, r.project, nullString(p.RunID), p.RunID, p.TaskID, p.SessionID, r.project, r.project, nullString(p.SupersedesID), p.SupersedesID, r.project)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func (r coordination) ListProgress(ctx context.Context, task string) ([]coord.Progress, error) {
	rows, e := r.tx.QueryContext(ctx, "SELECT p.id,p.task_id,p.run_id,p.session_id,p.phase,p.done_json,p.doing_json,p.next_json,p.created_at,p.supersedes_id FROM progress_updates p JOIN tasks t ON t.id=p.task_id WHERE p.task_id=? AND (?='legacy' OR t.project_id=?) ORDER BY p.created_at,p.id", task, r.project, r.project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanProgress(rows)
}
func (r coordination) GetProgress(ctx context.Context, id string) (coord.Progress, bool, error) {
	rows, e := r.tx.QueryContext(ctx, "SELECT p.id,p.task_id,p.run_id,p.session_id,p.phase,p.done_json,p.doing_json,p.next_json,p.created_at,p.supersedes_id FROM progress_updates p JOIN tasks t ON t.id=p.task_id WHERE p.id=? AND (?='legacy' OR t.project_id=?)", id, r.project, r.project)
	if e != nil {
		return coord.Progress{}, false, e
	}
	defer rows.Close()
	x, e := scanProgress(rows)
	if e != nil || len(x) == 0 {
		return coord.Progress{}, false, e
	}
	return x[0], true, nil
}
func (r coordination) CreateDependency(ctx context.Context, d coord.Dependency, at time.Time) error {
	res, e := r.tx.ExecContext(ctx, "INSERT INTO task_dependencies(id,blocker_task_id,blocked_task_id,kind,unblock_on,created_at) SELECT ?,?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?) AND EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?))", d.ID, d.PrerequisiteTaskID, d.DependentTaskID, d.Kind, d.Criterion, stamp(at), r.project, d.PrerequisiteTaskID, r.project, d.DependentTaskID, r.project)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func (r coordination) ListDependencies(ctx context.Context, project string) ([]coord.Dependency, error) {
	if !r.acceptsProject(project) {
		return nil, nil
	}
	rows, e := r.tx.QueryContext(ctx, "SELECT d.id,d.blocker_task_id,d.blocked_task_id,d.kind,d.unblock_on FROM task_dependencies d JOIN tasks t ON t.id=d.blocked_task_id JOIN tasks b ON b.id=d.blocker_task_id WHERE t.project_id=? AND b.project_id=? ORDER BY d.id", project, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []coord.Dependency{}
	for rows.Next() {
		var d coord.Dependency
		if e = rows.Scan(&d.ID, &d.PrerequisiteTaskID, &d.DependentTaskID, &d.Kind, &d.Criterion); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (r coordination) GetDependency(ctx context.Context, id string) (coord.Dependency, bool, error) {
	var d coord.Dependency
	e := r.tx.QueryRowContext(ctx, "SELECT d.id,d.blocker_task_id,d.blocked_task_id,d.kind,d.unblock_on FROM task_dependencies d JOIN tasks t ON t.id=d.blocked_task_id JOIN tasks b ON b.id=d.blocker_task_id WHERE d.id=? AND (?='legacy' OR (t.project_id=? AND b.project_id=?))", id, r.project, r.project, r.project).Scan(&d.ID, &d.PrerequisiteTaskID, &d.DependentTaskID, &d.Kind, &d.Criterion)
	if e == sql.ErrNoRows {
		return d, false, nil
	}
	return d, e == nil, e
}
func (r coordination) MarkDependencySatisfied(ctx context.Context, id string, at time.Time, evidence []byte, message string) (bool, error) {
	res, e := r.tx.ExecContext(ctx, "UPDATE task_dependencies SET satisfied_at=?,satisfaction_evidence_json=?,unblock_message_id=? WHERE id=? AND satisfied_at IS NULL AND (?='legacy' OR (EXISTS (SELECT 1 FROM tasks WHERE id=blocked_task_id AND project_id=?) AND EXISTS (SELECT 1 FROM tasks WHERE id=blocker_task_id AND project_id=?) AND (? IS NULL OR NOT EXISTS (SELECT 1 FROM messages WHERE id=?) OR EXISTS (SELECT 1 FROM messages WHERE id=? AND project_id=?))))", stamp(at), evidence, nullString(message), id, r.project, r.project, r.project, nullString(message), message, message, r.project)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
func (r coordination) HardDependenciesSatisfied(ctx context.Context, task string) (bool, error) {
	var ready int
	e := r.tx.QueryRowContext(ctx, "SELECT CASE WHEN EXISTS (SELECT 1 FROM tasks WHERE id=? AND (?='legacy' OR project_id=?)) AND NOT EXISTS (SELECT 1 FROM task_dependencies WHERE blocked_task_id=? AND kind='hard' AND satisfied_at IS NULL) THEN 1 ELSE 0 END", task, r.project, r.project, task).Scan(&ready)
	return ready == 1, e
}
func (r coordination) CreateMessage(ctx context.Context, project string, m coord.MailMessage) error {
	if !r.acceptsProject(project) {
		return fmt.Errorf("project mismatch")
	}
	res, e := r.tx.ExecContext(ctx, "INSERT INTO messages(id,thread_id,project_id,sender_session_id,related_task_id,type,subject,body,created_at) SELECT ?,?,?,?,?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?) AND (? IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?)))", m.ID, m.ThreadID, project, m.SenderSessionID, nullString(m.RelatedTaskID), m.Type, m.Subject, m.Body, stamp(m.CreatedAt), r.project, m.SenderSessionID, project, nullString(m.RelatedTaskID), m.RelatedTaskID, project)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	for i, t := range m.Recipients {
		s, h, task, role := targetColumns(t)
		res, e = r.tx.ExecContext(ctx, "INSERT INTO message_recipients(id,message_id,recipient_session_id,recipient_human_id,recipient_task_id,recipient_role,delivered_at) SELECT ?,?,?,?,?,?,? WHERE ?='legacy' OR ((? IS NULL OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM humans WHERE id=? AND (project_id=? OR (project_id IS NULL AND EXISTS (SELECT 1 FROM legacy_human_projects WHERE human_id=humans.id AND project_id=?))))) AND (? IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?)))", fmt.Sprintf("%s:%d", m.ID, i), m.ID, s, h, task, role, stamp(m.CreatedAt), r.project, s, s, project, h, h, project, project, task, task, project)
		if e != nil {
			return e
		}
		n, e = res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("project mismatch")
		}
	}
	return nil
}
func scanMessages(rows *sql.Rows) ([]coord.MailMessage, error) {
	out := []coord.MailMessage{}
	byID := map[string]int{}
	for rows.Next() {
		var m coord.MailMessage
		var related, s, h, t, role sql.NullString
		var at string
		if e := rows.Scan(&m.ID, &m.ThreadID, &m.SenderSessionID, &related, &m.Type, &m.Subject, &m.Body, &at, &s, &h, &t, &role); e != nil {
			return nil, e
		}
		parsed, e := parseP3BStamp(at)
		if e != nil {
			return nil, e
		}
		m.CreatedAt = parsed
		m.RelatedTaskID = related.String
		i, ok := byID[m.ID]
		if !ok {
			i = len(out)
			byID[m.ID] = i
			out = append(out, m)
		}
		out[i].Recipients = append(out[i].Recipients, coord.RecipientTarget{SessionID: s.String, HumanID: h.String, TaskID: t.String, Role: role.String})
	}
	return out, rows.Err()
}

const messageSelect = "SELECT m.id,m.thread_id,m.sender_session_id,m.related_task_id,m.type,m.subject,m.body,m.created_at,r.recipient_session_id,r.recipient_human_id,r.recipient_task_id,r.recipient_role FROM messages m JOIN message_recipients r ON r.message_id=m.id"

func (r coordination) GetMessage(ctx context.Context, id string) (coord.MailMessage, bool, error) {
	rows, e := r.tx.QueryContext(ctx, messageSelect+" WHERE m.id=? AND (?='legacy' OR m.project_id=?) ORDER BY m.created_at,m.id,r.id", id, r.project, r.project)
	if e != nil {
		return coord.MailMessage{}, false, e
	}
	defer rows.Close()
	x, e := scanMessages(rows)
	if e != nil || len(x) == 0 {
		return coord.MailMessage{}, false, e
	}
	return x[0], true, nil
}
func (r coordination) ListThread(ctx context.Context, thread string) ([]coord.MailMessage, error) {
	rows, e := r.tx.QueryContext(ctx, messageSelect+" WHERE m.thread_id=? AND (?='legacy' OR m.project_id=?) ORDER BY m.created_at,m.id,r.id", thread, r.project, r.project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanMessages(rows)
}
func (r coordination) ListInbox(ctx context.Context, project string, t coord.RecipientTarget) ([]coord.MailMessage, error) {
	if !r.acceptsProject(project) {
		return nil, nil
	}
	s, h, task, role := targetColumns(t)
	rows, e := r.tx.QueryContext(ctx, messageSelect+" WHERE m.project_id=? AND ((r.recipient_session_id IS ? AND r.recipient_session_id=?) OR (r.recipient_human_id IS ? AND r.recipient_human_id=?) OR (r.recipient_task_id IS ? AND r.recipient_task_id=?) OR (r.recipient_role IS ? AND r.recipient_role=?)) ORDER BY m.created_at,m.id,r.id", project, s, s, h, h, task, task, role, role)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scanMessages(rows)
}
func (r coordination) GetDelivery(ctx context.Context, id string, t coord.RecipientTarget) (coord.RecipientDelivery, bool, error) {
	s, h, task, role := targetColumns(t)
	var d coord.RecipientDelivery
	var delivered string
	var read, ack sql.NullString
	e := r.tx.QueryRowContext(ctx, "SELECT r.delivered_at,r.read_at,r.ack_at FROM message_recipients r JOIN messages m ON m.id=r.message_id WHERE r.message_id=? AND (?='legacy' OR m.project_id=?) AND ((r.recipient_session_id IS ? AND r.recipient_session_id=?) OR (r.recipient_human_id IS ? AND r.recipient_human_id=?) OR (r.recipient_task_id IS ? AND r.recipient_task_id=?) OR (r.recipient_role IS ? AND r.recipient_role=?))", id, r.project, r.project, s, s, h, h, task, task, role, role).Scan(&delivered, &read, &ack)
	if e == sql.ErrNoRows {
		return d, false, nil
	}
	if e != nil {
		return d, false, e
	}
	var err error
	d.MessageID = id
	d.Recipient = t
	d.DeliveredAt, err = parseP3BStamp(delivered)
	if err != nil {
		return d, false, err
	}
	if read.Valid {
		v, err := parseP3BStamp(read.String)
		if err != nil {
			return d, false, err
		}
		d.ReadAt = &v
	}
	if ack.Valid {
		v, err := parseP3BStamp(ack.String)
		if err != nil {
			return d, false, err
		}
		d.AckedAt = &v
	}
	return d, true, nil
}
func (r coordination) GetDeliveryRowID(ctx context.Context, messageID string, t coord.RecipientTarget) (string, bool, error) {
	s, h, task, role := targetColumns(t)
	var rowID string
	e := r.tx.QueryRowContext(ctx, "SELECT r.id FROM message_recipients r JOIN messages m ON m.id=r.message_id WHERE r.message_id=? AND (?='legacy' OR m.project_id=?) AND ((r.recipient_session_id IS ? AND r.recipient_session_id=?) OR (r.recipient_human_id IS ? AND r.recipient_human_id=?) OR (r.recipient_task_id IS ? AND r.recipient_task_id=?) OR (r.recipient_role IS ? AND r.recipient_role=?))", messageID, r.project, r.project, s, s, h, h, task, task, role, role).Scan(&rowID)
	if e == sql.ErrNoRows {
		return "", false, nil
	}
	return rowID, e == nil, e
}
func (r coordination) GetDeliveryByID(ctx context.Context, recipientID string) (coord.RecipientDelivery, bool, error) {
	var d coord.RecipientDelivery
	var s, h, task, role sql.NullString
	var delivered string
	var read, ack sql.NullString
	e := r.tx.QueryRowContext(ctx, "SELECT r.message_id,r.recipient_session_id,r.recipient_human_id,r.recipient_task_id,r.recipient_role,r.delivered_at,r.read_at,r.ack_at FROM message_recipients r JOIN messages m ON m.id=r.message_id WHERE r.id=? AND (?='legacy' OR m.project_id=?)", recipientID, r.project, r.project).Scan(&d.MessageID, &s, &h, &task, &role, &delivered, &read, &ack)
	if e == sql.ErrNoRows {
		return d, false, nil
	}
	if e != nil {
		return d, false, e
	}
	d.Recipient = coord.RecipientTarget{SessionID: s.String, HumanID: h.String, TaskID: task.String, Role: role.String}
	var err error
	d.DeliveredAt, err = parseP3BStamp(delivered)
	if err != nil {
		return d, false, err
	}
	if read.Valid {
		v, e := parseP3BStamp(read.String)
		if e != nil {
			return d, false, e
		}
		d.ReadAt = &v
	}
	if ack.Valid {
		v, e := parseP3BStamp(ack.String)
		if e != nil {
			return d, false, e
		}
		d.AckedAt = &v
	}
	return d, true, nil
}
func (r coordination) SetDelivery(ctx context.Context, d coord.RecipientDelivery) error {
	s, h, task, role := targetColumns(d.Recipient)
	res, e := r.tx.ExecContext(ctx, "UPDATE message_recipients SET delivered_at=?,read_at=?,ack_at=? WHERE message_id=? AND (?='legacy' OR EXISTS (SELECT 1 FROM messages WHERE id=message_recipients.message_id AND project_id=?)) AND ((recipient_session_id IS ? AND recipient_session_id=?) OR (recipient_human_id IS ? AND recipient_human_id=?) OR (recipient_task_id IS ? AND recipient_task_id=?) OR (recipient_role IS ? AND recipient_role=?))", stamp(d.DeliveredAt), nullTime(d.ReadAt), nullTime(d.AckedAt), d.MessageID, r.project, r.project, s, s, h, h, task, task, role, role)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func handoffJSON(h coord.Handoff) ([]byte, []byte, []byte, []byte, []byte, error) {
	a, e := encodeJSON(h.ChangedFiles)
	if e != nil {
		return nil, nil, nil, nil, nil, e
	}
	b, e := encodeJSON(h.Commits)
	if e != nil {
		return nil, nil, nil, nil, nil, e
	}
	c, e := encodeJSON(h.VerificationEvidence)
	if e != nil {
		return nil, nil, nil, nil, nil, e
	}
	d, e := encodeJSON(h.RemainingRisks)
	if e != nil {
		return nil, nil, nil, nil, nil, e
	}
	f, e := encodeJSON(h.SuggestedActions)
	return a, b, c, d, f, e
}
func (r coordination) CreateHandoff(ctx context.Context, h coord.Handoff) error {
	a, b, c, d, f, e := handoffJSON(h)
	if e != nil {
		return e
	}
	res, e := r.tx.ExecContext(ctx, "INSERT INTO handoffs(id,task_id,target_task_id,run_id,source_session_id,target_session_id,supersedes_id,summary,final_output_text,final_output_hash,final_output_policy,changed_files_json,commits_json,verification_json,risks_json,actions_json,status,created_at) SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?) AND EXISTS (SELECT 1 FROM task_runs r JOIN tasks t ON t.id=r.task_id JOIN agent_sessions s ON s.id=r.session_id WHERE r.id=? AND r.task_id=? AND r.session_id=? AND t.project_id=? AND s.project_id=?) AND (? IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM handoffs h JOIN tasks t ON t.id=h.task_id WHERE h.id=? AND t.project_id=?)))", h.ID, h.TaskID, nullString(h.TargetTaskID), h.RunID, h.SourceSessionID, nullString(h.TargetSessionID), nullString(h.SupersedesID), h.Summary, nullString(h.FinalOutput.Text), nullString(h.FinalOutput.Hash), h.FinalOutput.Policy, a, b, c, d, f, h.Status, stamp(h.CreatedAt), r.project, h.TaskID, r.project, h.RunID, h.TaskID, h.SourceSessionID, r.project, r.project, nullString(h.TargetTaskID), h.TargetTaskID, r.project, nullString(h.TargetSessionID), h.TargetSessionID, r.project, nullString(h.SupersedesID), h.SupersedesID, r.project)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func scanHandoff(row *sql.Row) (coord.Handoff, bool, error) {
	var h coord.Handoff
	var target, run, ts, sup, text, hash, policy sql.NullString
	var a, b, c, d, f []byte
	var at string
	e := row.Scan(&h.ID, &h.TaskID, &target, &run, &h.SourceSessionID, &ts, &sup, &h.Summary, &text, &hash, &policy, &a, &b, &c, &d, &f, &h.Status, &at)
	if e == sql.ErrNoRows {
		return h, false, nil
	}
	if e != nil {
		return h, false, e
	}
	var err error
	h.CreatedAt, err = parseP3BStamp(at)
	if err != nil {
		return h, false, err
	}
	h.TargetTaskID = target.String
	h.RunID = run.String
	h.TargetSessionID = ts.String
	h.SupersedesID = sup.String
	h.FinalOutput = coord.SensitiveText{Text: text.String, Hash: hash.String, Policy: coord.FinalOutputPolicy(policy.String)}
	if err = json.Unmarshal(a, &h.ChangedFiles); err != nil {
		return h, false, err
	}
	if err = json.Unmarshal(b, &h.Commits); err != nil {
		return h, false, err
	}
	if err = json.Unmarshal(c, &h.VerificationEvidence); err != nil {
		return h, false, err
	}
	if err = json.Unmarshal(d, &h.RemainingRisks); err != nil {
		return h, false, err
	}
	if err = json.Unmarshal(f, &h.SuggestedActions); err != nil {
		return h, false, err
	}
	return h, true, nil
}

const handoffSelect = "SELECT h.id,h.task_id,h.target_task_id,h.run_id,h.source_session_id,h.target_session_id,h.supersedes_id,h.summary,h.final_output_text,h.final_output_hash,h.final_output_policy,h.changed_files_json,h.commits_json,h.verification_json,h.risks_json,h.actions_json,h.status,h.created_at FROM handoffs"

func (r coordination) GetHandoff(ctx context.Context, id string) (coord.Handoff, bool, error) {
	return scanHandoff(r.tx.QueryRowContext(ctx, handoffSelect+" h JOIN tasks t ON t.id=h.task_id WHERE h.id=? AND (?='legacy' OR t.project_id=?)", id, r.project, r.project))
}
func (r coordination) ListHandoffs(ctx context.Context, task string) ([]coord.Handoff, error) {
	rows, e := r.tx.QueryContext(ctx, "SELECT h.id FROM handoffs h JOIN tasks t ON t.id=h.task_id WHERE h.task_id=? AND (?='legacy' OR t.project_id=?) ORDER BY h.created_at,h.id", task, r.project, r.project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []coord.Handoff{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		h, ok, e := r.GetHandoff(ctx, id)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, fmt.Errorf("invalid stored handoff")
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (r coordination) CreateHandoffDecision(ctx context.Context, d coord.HandoffDecision) error {
	res, e := r.tx.ExecContext(ctx, "INSERT INTO handoff_decisions(id,handoff_id,decision,decided_by_session_id,created_at) SELECT ?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM handoffs h JOIN tasks t ON t.id=h.task_id WHERE h.id=? AND t.project_id=?) AND EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?))", d.ID, d.HandoffID, d.Decision, d.DecidedBySessionID, stamp(d.CreatedAt), r.project, d.HandoffID, r.project, d.DecidedBySessionID, r.project)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func (r coordination) GetHandoffDecision(ctx context.Context, id string) (coord.HandoffDecision, bool, error) {
	var d coord.HandoffDecision
	var at string
	e := r.tx.QueryRowContext(ctx, "SELECT d.id,d.handoff_id,d.decision,d.decided_by_session_id,d.created_at FROM handoff_decisions d JOIN handoffs h ON h.id=d.handoff_id JOIN tasks t ON t.id=h.task_id WHERE d.handoff_id=? AND (?='legacy' OR t.project_id=?)", id, r.project, r.project).Scan(&d.ID, &d.HandoffID, &d.Decision, &d.DecidedBySessionID, &at)
	if e == sql.ErrNoRows {
		return d, false, nil
	}
	if e != nil {
		return d, false, e
	}
	var err error
	d.CreatedAt, err = parseP3BStamp(at)
	return d, err == nil, err
}
func (r coordination) GetHandoffDecisionByID(ctx context.Context, id string) (coord.HandoffDecision, bool, error) {
	var d coord.HandoffDecision
	var at string
	e := r.tx.QueryRowContext(ctx, "SELECT d.id,d.handoff_id,d.decision,d.decided_by_session_id,d.created_at FROM handoff_decisions d JOIN handoffs h ON h.id=d.handoff_id JOIN tasks t ON t.id=h.task_id WHERE d.id=? AND (?='legacy' OR t.project_id=?)", id, r.project, r.project).Scan(&d.ID, &d.HandoffID, &d.Decision, &d.DecidedBySessionID, &at)
	if e == sql.ErrNoRows {
		return d, false, nil
	}
	if e != nil {
		return d, false, e
	}
	var err error
	d.CreatedAt, err = parseP3BStamp(at)
	return d, err == nil, err
}
func (r coordination) CreateHandoffLifecycleEvent(ctx context.Context, event coord.HandoffLifecycleEvent) error {
	res, err := r.tx.ExecContext(ctx, `INSERT INTO handoff_lifecycle_events(
		id,handoff_id,state,actor_session_id,source_commit,source_tree,integration_commit,
		canary_run_id,canary_integration_ref,canary_target_sha,canary_target_tree,canary_result,
		canary_command,canary_execution_kind,canary_environment_fingerprint,canary_head_before,canary_head_after,
		canary_ref_fingerprint_before,canary_ref_fingerprint_after,canary_exit_code,
		canary_passed_count,canary_failed_count,canary_skipped_count,canary_started_at,canary_finished_at,canary_evidence_path,
		source_worktree_cleaned,source_branch_cleaned,note,created_at
	) SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE ?='legacy' OR (
		EXISTS (SELECT 1 FROM handoffs h JOIN tasks t ON t.id=h.task_id WHERE h.id=? AND t.project_id=?)
		AND EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?))`,
		event.ID, event.HandoffID, event.State, event.ActorSessionID, nullString(event.SourceCommit), nullString(event.SourceTree), nullString(event.IntegrationCommit),
		nullString(event.CanaryRunID), nullString(event.CanaryIntegrationRef), nullString(event.CanaryTargetSHA), nullString(event.CanaryTargetTree), nullString(event.CanaryResult),
		nullString(event.CanaryCommand), nullString(event.CanaryExecutionKind), nullString(event.CanaryEnvironmentFingerprint), nullString(event.CanaryHeadBefore), nullString(event.CanaryHeadAfter),
		nullString(event.CanaryRefFingerprintBefore), nullString(event.CanaryRefFingerprintAfter), nullInt(event.CanaryExitCode),
		event.CanaryPassedCount, event.CanaryFailedCount, event.CanarySkippedCount, nullTime(event.CanaryStartedAt), nullTime(event.CanaryFinishedAt), nullString(event.CanaryEvidencePath),
		event.SourceWorktreeCleaned, event.SourceBranchCleaned, nullString(event.Note), stamp(event.CreatedAt),
		r.project, event.HandoffID, r.project, event.ActorSessionID, r.project)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}

func scanHandoffLifecycleEvent(row *sql.Row) (coord.HandoffLifecycleEvent, bool, error) {
	var event coord.HandoffLifecycleEvent
	var sourceCommit, sourceTree, integrationCommit, canaryRunID, canaryIntegrationRef, canaryTarget, canaryTargetTree, canaryResult sql.NullString
	var canaryCommand, canaryExecutionKind, canaryEnvironmentFingerprint, canaryHeadBefore, canaryHeadAfter sql.NullString
	var canaryRefFingerprintBefore, canaryRefFingerprintAfter, canaryStartedAt, canaryFinishedAt, canaryEvidencePath, note sql.NullString
	var canaryExitCode sql.NullInt64
	var createdAt string
	err := row.Scan(&event.ID, &event.HandoffID, &event.State, &event.ActorSessionID, &sourceCommit, &sourceTree, &integrationCommit,
		&canaryRunID, &canaryIntegrationRef, &canaryTarget, &canaryTargetTree, &canaryResult, &canaryCommand, &canaryExecutionKind, &canaryEnvironmentFingerprint,
		&canaryHeadBefore, &canaryHeadAfter, &canaryRefFingerprintBefore, &canaryRefFingerprintAfter, &canaryExitCode,
		&event.CanaryPassedCount, &event.CanaryFailedCount, &event.CanarySkippedCount, &canaryStartedAt, &canaryFinishedAt, &canaryEvidencePath,
		&event.SourceWorktreeCleaned, &event.SourceBranchCleaned, &note, &createdAt)
	if err == sql.ErrNoRows {
		return event, false, nil
	}
	if err != nil {
		return event, false, err
	}
	event.SourceCommit = sourceCommit.String
	event.SourceTree = sourceTree.String
	event.IntegrationCommit = integrationCommit.String
	event.CanaryRunID = canaryRunID.String
	event.CanaryIntegrationRef = canaryIntegrationRef.String
	event.CanaryTargetSHA = canaryTarget.String
	event.CanaryTargetTree = canaryTargetTree.String
	event.CanaryResult = canaryResult.String
	event.CanaryCommand = canaryCommand.String
	event.CanaryExecutionKind = canaryExecutionKind.String
	event.CanaryEnvironmentFingerprint = canaryEnvironmentFingerprint.String
	event.CanaryHeadBefore = canaryHeadBefore.String
	event.CanaryHeadAfter = canaryHeadAfter.String
	event.CanaryRefFingerprintBefore = canaryRefFingerprintBefore.String
	event.CanaryRefFingerprintAfter = canaryRefFingerprintAfter.String
	if canaryExitCode.Valid {
		value := int(canaryExitCode.Int64)
		event.CanaryExitCode = &value
	}
	if canaryStartedAt.Valid {
		value, parseErr := parseP3BStamp(canaryStartedAt.String)
		if parseErr != nil {
			return event, false, parseErr
		}
		event.CanaryStartedAt = &value
	}
	if canaryFinishedAt.Valid {
		value, parseErr := parseP3BStamp(canaryFinishedAt.String)
		if parseErr != nil {
			return event, false, parseErr
		}
		event.CanaryFinishedAt = &value
	}
	event.CanaryEvidencePath = canaryEvidencePath.String
	event.Note = note.String
	event.CreatedAt, err = parseP3BStamp(createdAt)
	return event, err == nil, err
}

const handoffLifecycleSelect = `SELECT e.id,e.handoff_id,e.state,e.actor_session_id,e.source_commit,e.source_tree,e.integration_commit,
e.canary_run_id,e.canary_integration_ref,e.canary_target_sha,e.canary_target_tree,e.canary_result,
e.canary_command,e.canary_execution_kind,e.canary_environment_fingerprint,e.canary_head_before,e.canary_head_after,
e.canary_ref_fingerprint_before,e.canary_ref_fingerprint_after,e.canary_exit_code,
e.canary_passed_count,e.canary_failed_count,e.canary_skipped_count,e.canary_started_at,e.canary_finished_at,e.canary_evidence_path,
e.source_worktree_cleaned,e.source_branch_cleaned,e.note,e.created_at FROM handoff_lifecycle_events e`

func (r coordination) GetHandoffLifecycleEventByID(ctx context.Context, id string) (coord.HandoffLifecycleEvent, bool, error) {
	return scanHandoffLifecycleEvent(r.tx.QueryRowContext(ctx, handoffLifecycleSelect+` JOIN handoffs h ON h.id=e.handoff_id JOIN tasks t ON t.id=h.task_id WHERE e.id=? AND (?='legacy' OR t.project_id=?)`, id, r.project, r.project))
}

func (r coordination) ListHandoffLifecycleEvents(ctx context.Context, handoffID string) ([]coord.HandoffLifecycleEvent, error) {
	rows, err := r.tx.QueryContext(ctx, `SELECT e.id FROM handoff_lifecycle_events e JOIN handoffs h ON h.id=e.handoff_id JOIN tasks t ON t.id=h.task_id WHERE e.handoff_id=? AND (?='legacy' OR t.project_id=?) ORDER BY e.created_at,e.rowid`, handoffID, r.project, r.project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []coord.HandoffLifecycleEvent{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		event, ok, err := r.GetHandoffLifecycleEventByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("invalid stored handoff lifecycle event")
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
func (r coordination) CreateAdoption(ctx context.Context, a coord.Adoption) error {
	if !r.acceptsProject(a.ProjectID) {
		return fmt.Errorf("project mismatch")
	}
	res, e := r.tx.ExecContext(ctx, "INSERT INTO adoptions(id,project_id,adopter_session_id,orphan_session_id,orphan_task_id,orphan_handoff_id,git_asset_ref,reason,created_at) SELECT ?,?,?,?,?,?,?,?,? WHERE ?='legacy' OR (EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?) AND (? IS NULL OR EXISTS (SELECT 1 FROM agent_sessions WHERE id=? AND project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id=? AND project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM handoffs h JOIN tasks t ON t.id=h.task_id WHERE h.id=? AND t.project_id=?)) AND (? IS NULL OR EXISTS (SELECT 1 FROM git_observations o JOIN git_observation_assets ga ON ga.observation_id=o.id WHERE ga.fingerprint=? AND o.project_id=?)))", a.ID, a.ProjectID, a.NewOwnerSessionID, nullString(a.SessionID), nullString(a.TaskID), nullString(a.HandoffID), nullString(a.GitAssetID), a.Reason, stamp(a.CreatedAt), r.project, a.NewOwnerSessionID, a.ProjectID, nullString(a.SessionID), a.SessionID, a.ProjectID, nullString(a.TaskID), a.TaskID, a.ProjectID, nullString(a.HandoffID), a.HandoffID, a.ProjectID, nullString(a.GitAssetID), a.GitAssetID, a.ProjectID)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("project mismatch")
	}
	return nil
}
func (r coordination) GetAdoptionByID(ctx context.Context, id string) (coord.Adoption, bool, error) {
	var a coord.Adoption
	var session, task, handoff, asset sql.NullString
	var at string
	err := r.tx.QueryRowContext(ctx, "SELECT id,project_id,adopter_session_id,orphan_session_id,orphan_task_id,orphan_handoff_id,git_asset_ref,reason,created_at FROM adoptions WHERE id=? AND (?='legacy' OR project_id=?)", id, r.project, r.project).Scan(&a.ID, &a.ProjectID, &a.NewOwnerSessionID, &session, &task, &handoff, &asset, &a.Reason, &at)
	if err == sql.ErrNoRows {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	a.SessionID, a.TaskID, a.HandoffID, a.GitAssetID = session.String, task.String, handoff.String, asset.String
	a.CreatedAt, err = parseP3BStamp(at)
	if err != nil || a.Validate() != nil {
		return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption")
	}
	owner, ok, err := r.GetSession(ctx, lineage.ID(a.NewOwnerSessionID))
	if err != nil || !ok || string(owner.ProjectID) != a.ProjectID {
		return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption owner")
	}
	if a.SessionID != "" {
		target, ok, err := r.GetSession(ctx, lineage.ID(a.SessionID))
		if err != nil || !ok || string(target.ProjectID) != a.ProjectID {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption target")
		}
	}
	if a.TaskID != "" {
		target, ok, err := r.GetTask(ctx, lineage.ID(a.TaskID))
		if err != nil || !ok || string(target.ProjectID) != a.ProjectID {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption target")
		}
	}
	if a.HandoffID != "" {
		target, ok, err := r.GetHandoff(ctx, a.HandoffID)
		if err != nil || !ok {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption target")
		}
		linked, ok, err := r.GetTask(ctx, lineage.ID(target.TaskID))
		if err != nil || !ok || string(linked.ProjectID) != a.ProjectID {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored adoption target")
		}
	}
	return a, true, nil
}
func (r coordination) LatestGitAdoption(ctx context.Context, project, fingerprint string) (coord.Adoption, bool, error) {
	if !r.acceptsProject(project) {
		return coord.Adoption{}, false, nil
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT id FROM adoptions WHERE project_id=? AND git_asset_ref IS NOT NULL`, project)
	if err != nil {
		return coord.Adoption{}, false, err
	}
	defer rows.Close()
	var latest coord.Adoption
	found := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return coord.Adoption{}, false, err
		}
		adoption, ok, err := r.GetAdoptionByID(ctx, id)
		if err != nil {
			return coord.Adoption{}, false, err
		}
		if !ok || adoption.ProjectID != project {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored git adoption")
		}
		adoptedAt, err := fixedUTC(adoption.CreatedAt)
		if err != nil {
			return coord.Adoption{}, false, err
		}
		var exists int
		if err := r.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM git_observations o JOIN git_observation_assets a ON a.observation_id=o.id WHERE o.project_id=? AND a.fingerprint=? AND o.observed_at<=?)`, project, adoption.GitAssetID, adoptedAt).Scan(&exists); err != nil {
			return coord.Adoption{}, false, err
		}
		if exists != 1 {
			return coord.Adoption{}, false, fmt.Errorf("invalid stored git adoption")
		}
		if adoption.GitAssetID != fingerprint {
			continue
		}
		if !found || adoption.CreatedAt.After(latest.CreatedAt) || (adoption.CreatedAt.Equal(latest.CreatedAt) && adoption.ID > latest.ID) {
			latest, found = adoption, true
		}
	}
	if err := rows.Err(); err != nil {
		return coord.Adoption{}, false, err
	}
	return latest, found, nil
}
