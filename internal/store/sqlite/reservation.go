package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/domain/reservation"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

//go:embed migrations/0003_reservations.sql
var reservationFS embed.FS

var reservationSQL = mustReservationSQL()

func mustReservationSQL() string {
	b, err := reservationFS.ReadFile("migrations/0003_reservations.sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (r repositories) Reservations() ports.ReservationRepository {
	return reservationRepo{tx: r.tx, project: r.project}
}

type reservationRepo struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r reservationRepo) acceptsProject(project domain.ProjectID) bool {
	return r.project == "legacy" || project == r.project
}

func utc(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

const reservationBaseColumns = `r.id,r.project_id,r.human_id,r.session_id,r.task_id,r.run_id,r.pattern_kind,r.normalized_pattern,r.case_sensitivity,r.mode,r.intent,r.expires_at`
const reservationProjection = `,COALESCE((SELECT expires_at FROM reservation_renewals WHERE reservation_id=r.id ORDER BY occurred_at DESC,id DESC LIMIT 1),r.expires_at),CASE WHEN EXISTS(SELECT 1 FROM reservation_releases WHERE reservation_id=r.id) THEN 'released' WHEN EXISTS(SELECT 1 FROM reservation_overrides WHERE reservation_id=r.id) THEN 'overridden' ELSE 'active' END,(SELECT occurred_at FROM reservation_releases WHERE reservation_id=r.id)`

func scanReservation(row interface{ Scan(...any) error }) (reservation.Reservation, domain.ProjectID, bool, error) {
	var id, project, human, session, task, run, kind, pattern, cs, mode, intent, baseExpires, expires, lifecycle string
	var released sql.NullString
	if err := row.Scan(&id, &project, &human, &session, &task, &run, &kind, &pattern, &cs, &mode, &intent, &baseExpires, &expires, &lifecycle, &released); errors.Is(err, sql.ErrNoRows) {
		return reservation.Reservation{}, "", false, nil
	} else if err != nil {
		return reservation.Reservation{}, "", false, err
	}
	baseExpiry, err := time.Parse(time.RFC3339Nano, baseExpires)
	if err != nil {
		return reservation.Reservation{}, "", false, err
	}
	current, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return reservation.Reservation{}, "", false, err
	}
	base, err := reservation.New(reservation.ReservationInput{ID: id, Pattern: reservation.Pattern{Kind: reservation.PatternKind(kind), Value: pattern, CaseSensitivity: reservation.CaseSensitivity(cs)}, Mode: reservation.Mode(mode), Owner: reservation.Owner{HumanID: human, SessionID: session, TaskID: task, RunID: run}, Intent: intent, ExpiresAt: baseExpiry})
	if err != nil {
		return reservation.Reservation{}, "", false, err
	}
	if current.After(base.ExpiresAt) {
		base, _, err = base.Renew(time.Time{}, current)
		if err != nil {
			return reservation.Reservation{}, "", false, err
		}
	}
	switch reservation.Lifecycle(lifecycle) {
	case reservation.Active:
	case reservation.Released:
		at, err := time.Parse(time.RFC3339Nano, released.String)
		if err != nil {
			return reservation.Reservation{}, "", false, err
		}
		base, _, err = base.Release(at, "derived release")
	case reservation.Overridden:
		base, _, err = base.Override(time.Time{}, reservation.OverrideRecord{HumanID: "derived", Reason: "derived override"})
	default:
		err = fmt.Errorf("invalid derived reservation lifecycle")
	}
	if err != nil {
		return reservation.Reservation{}, "", false, err
	}
	return base, domain.ProjectID(project), true, nil
}

func (r reservationRepo) Create(ctx context.Context, p domain.ProjectID, v reservation.Reservation, at time.Time) error {
	if !r.acceptsProject(p) {
		return errors.New("project mismatch")
	}
	_, err := r.tx.ExecContext(ctx, `INSERT INTO reservations(id,project_id,human_id,session_id,task_id,run_id,pattern_kind,normalized_pattern,case_sensitivity,mode,intent,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, p, v.Owner.HumanID, v.Owner.SessionID, v.Owner.TaskID, v.Owner.RunID, v.Pattern.Kind, v.Pattern.Value, v.Pattern.CaseSensitivity, v.Mode, v.Intent, utc(v.ExpiresAt), utc(at))
	return err
}

func (r reservationRepo) Get(ctx context.Context, p domain.ProjectID, id string) (reservation.Reservation, bool, error) {
	if !r.acceptsProject(p) {
		return reservation.Reservation{}, false, errors.New("project mismatch")
	}
	v, _, ok, err := scanReservation(r.tx.QueryRowContext(ctx, `SELECT `+reservationBaseColumns+reservationProjection+` FROM reservations r WHERE r.project_id=? AND r.id=?`, p, id))
	return v, ok, err
}

func (r reservationRepo) List(ctx context.Context, p domain.ProjectID) ([]reservation.Reservation, error) {
	if !r.acceptsProject(p) {
		return nil, errors.New("project mismatch")
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT `+reservationBaseColumns+reservationProjection+` FROM reservations r WHERE r.project_id=? ORDER BY r.created_at,r.id`, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []reservation.Reservation{}
	for rows.Next() {
		v, _, _, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func insertedOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("reservation target unavailable")
	}
	return nil
}

func (r reservationRepo) Renew(ctx context.Context, p domain.ProjectID, id string, f reservation.RenewalFact, at time.Time) error {
	if !r.acceptsProject(p) {
		return errors.New("project mismatch")
	}
	result, err := r.tx.ExecContext(ctx, `INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) SELECT ?,r.id,?,?,?,?,? FROM reservations r WHERE r.project_id=? AND r.id=?`, newID("reservation-renewal", id+f.CheckpointID, at), f.CheckpointID, utc(f.Previous), utc(f.ExpiresAt), utc(f.At), p, id)
	return insertedOne(result, err)
}

func (r reservationRepo) Release(ctx context.Context, p domain.ProjectID, id string, f reservation.ReleaseFact) error {
	if !r.acceptsProject(p) {
		return errors.New("project mismatch")
	}
	result, err := r.tx.ExecContext(ctx, `INSERT INTO reservation_releases(id,reservation_id,reason,occurred_at) SELECT ?,r.id,?,? FROM reservations r WHERE r.project_id=? AND r.id=?`, newID("reservation-release", id+f.Reason, f.At), f.Reason, utc(f.At), p, id)
	return insertedOne(result, err)
}

func (r reservationRepo) Override(ctx context.Context, p domain.ProjectID, id string, f reservation.OverrideFact) error {
	if !r.acceptsProject(p) {
		return errors.New("project mismatch")
	}
	result, err := r.tx.ExecContext(ctx, `INSERT INTO reservation_overrides(id,reservation_id,human_id,reason,occurred_at) SELECT ?,r.id,?,?,? FROM reservations r WHERE r.project_id=? AND r.id=?`, newID("reservation-override", id+f.Record.HumanID, f.At), f.Record.HumanID, f.Record.Reason, utc(f.At), p, id)
	return insertedOne(result, err)
}

func (r reservationRepo) History(ctx context.Context, p domain.ProjectID, id string) (reservation.ReservationHistory, bool, error) {
	if !r.acceptsProject(p) {
		return reservation.ReservationHistory{}, false, errors.New("project mismatch")
	}
	var baseID, project, human, session, task, run, kind, pattern, cs, mode, intent, expires, created string
	err := r.tx.QueryRowContext(ctx, `SELECT id,project_id,human_id,session_id,task_id,run_id,pattern_kind,normalized_pattern,case_sensitivity,mode,intent,expires_at,created_at FROM reservations WHERE project_id=? AND id=?`, p, id).Scan(&baseID, &project, &human, &session, &task, &run, &kind, &pattern, &cs, &mode, &intent, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return reservation.ReservationHistory{}, false, nil
	}
	if err != nil {
		return reservation.ReservationHistory{}, false, err
	}
	baseExpiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return reservation.ReservationHistory{}, false, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil || !baseExpiry.After(createdAt) {
		return reservation.ReservationHistory{}, false, fmt.Errorf("invalid reservation base chronology")
	}
	base, err := reservation.New(reservation.ReservationInput{ID: baseID, Pattern: reservation.Pattern{Kind: reservation.PatternKind(kind), Value: pattern, CaseSensitivity: reservation.CaseSensitivity(cs)}, Mode: reservation.Mode(mode), Owner: reservation.Owner{HumanID: human, SessionID: session, TaskID: task, RunID: run}, Intent: intent, ExpiresAt: baseExpiry})
	if err != nil {
		return reservation.ReservationHistory{}, false, err
	}
	history := reservation.ReservationHistory{Base: base, Current: base, Renewals: []reservation.RenewalFact{}}
	rows, err := r.tx.QueryContext(ctx, `SELECT checkpoint_id,previous_expires_at,expires_at,occurred_at FROM reservation_renewals WHERE reservation_id=? ORDER BY occurred_at,id`, id)
	lastOccurred := createdAt
	if err != nil {
		return reservation.ReservationHistory{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var checkpoint, previous, next, occurred string
		if err := rows.Scan(&checkpoint, &previous, &next, &occurred); err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		factAt, err := time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		previousAt, err := time.Parse(time.RFC3339Nano, previous)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		nextAt, err := time.Parse(time.RFC3339Nano, next)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		fact := reservation.RenewalFact{At: factAt, Previous: previousAt, ExpiresAt: nextAt, CheckpointID: checkpoint}
		if fact.Previous != history.Current.ExpiresAt || !fact.At.After(lastOccurred) || !fact.At.Before(history.Current.ExpiresAt) {
			return reservation.ReservationHistory{}, false, fmt.Errorf("invalid reservation renewal history")
		}
		history.Current, _, err = history.Current.Renew(fact.At, fact.ExpiresAt)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		lastOccurred = fact.At
		history.Renewals = append(history.Renewals, fact)
	}
	if err := rows.Err(); err != nil {
		return reservation.ReservationHistory{}, false, err
	}
	var releaseAt, reason string
	err = r.tx.QueryRowContext(ctx, `SELECT occurred_at,reason FROM reservation_releases WHERE reservation_id=?`, id).Scan(&releaseAt, &reason)
	if err == nil {
		at, err := time.Parse(time.RFC3339Nano, releaseAt)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		if !at.After(lastOccurred) || !at.Before(history.Current.ExpiresAt) {
			return reservation.ReservationHistory{}, false, fmt.Errorf("invalid reservation release history")
		}
		fact := reservation.ReleaseFact{At: at, Reason: reason}
		history.Current, _, err = history.Current.Release(fact.At, fact.Reason)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		history.Release = &fact
	} else if !errors.Is(err, sql.ErrNoRows) {
		return reservation.ReservationHistory{}, false, err
	}
	var overrideAt, overrideHuman, overrideReason string
	err = r.tx.QueryRowContext(ctx, `SELECT occurred_at,human_id,reason FROM reservation_overrides WHERE reservation_id=?`, id).Scan(&overrideAt, &overrideHuman, &overrideReason)
	if err == nil {
		at, err := time.Parse(time.RFC3339Nano, overrideAt)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		if !at.After(lastOccurred) || !at.Before(history.Current.ExpiresAt) {
			return reservation.ReservationHistory{}, false, fmt.Errorf("invalid reservation override history")
		}
		fact := reservation.OverrideFact{At: at, Record: reservation.OverrideRecord{HumanID: overrideHuman, Reason: overrideReason}}
		history.Current, _, err = history.Current.Override(fact.At, fact.Record)
		if err != nil {
			return reservation.ReservationHistory{}, false, err
		}
		history.Override = &fact
	} else if !errors.Is(err, sql.ErrNoRows) {
		return reservation.ReservationHistory{}, false, err
	}
	return history, true, nil
}

func (r reservationRepo) ReleaseForTask(ctx context.Context, p domain.ProjectID, task lineage.ID, at time.Time, reason string) ([]reservation.Reservation, error) {
	if !r.acceptsProject(p) {
		return nil, errors.New("project mismatch")
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT `+reservationBaseColumns+reservationProjection+` FROM reservations r WHERE r.project_id=? AND r.task_id=? AND NOT EXISTS(SELECT 1 FROM reservation_releases WHERE reservation_id=r.id) AND NOT EXISTS(SELECT 1 FROM reservation_overrides WHERE reservation_id=r.id) AND COALESCE((SELECT expires_at FROM reservation_renewals WHERE reservation_id=r.id ORDER BY occurred_at DESC,id DESC LIMIT 1),r.expires_at)>? ORDER BY r.created_at,r.id`, p, task, utc(at))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []reservation.Reservation{}
	for rows.Next() {
		v, _, _, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		if err := r.Release(ctx, p, v.ID, reservation.ReleaseFact{At: at, Reason: reason}); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
