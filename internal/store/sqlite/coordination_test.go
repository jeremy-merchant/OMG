package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestCoordinationClaimRaceHasOneWinner(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Now().UTC()
	if _, e := s.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "p", now.Format(time.RFC3339Nano)); e != nil {
		t.Fatal(e)
	}
	_, _, e := s.Write(ctx, "seed", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		session := core.AgentSession{ID: "s", ProjectID: "p", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "s", StartedAt: now}
		if e := c.CreateSession(ctx, session); e != nil {
			return domain.Result{}, e
		}
		task := core.Task{ID: "t", ProjectID: "p", Title: "claim", State: core.TaskReady, CreatedAt: now, UpdatedAt: now}
		_, e := c.CreateTask(ctx, task)
		return domain.Result{ID: "seed", Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	winners := make(chan bool, 32)
	errs := make(chan error, 32)
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, e := s.Write(ctx, domain.IdempotencyKey("claim-"+string(rune('a'+i))), "test.write", func(r ports.Repositories) (domain.Result, error) {
				_, won, e := r.Coordination().ClaimTask(ctx, "t", "s", time.Now().UTC())
				if e != nil {
					return domain.Result{}, e
				}
				if !won {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "lineage ownership conflict", false)
				}
				return domain.Result{ID: "t", Outcome: domain.OutcomeOK}, nil
			})
			errs <- e
			winners <- e == nil
		}(i)
	}
	wg.Wait()
	close(winners)
	close(errs)
	count := 0
	for winner := range winners {
		if winner {
			count++
		}
	}
	if count != 1 {
		for e := range errs {
			if e != nil {
				t.Log(e)
			}
		}
		t.Fatalf("winners = %d; want 1", count)
	}
}

func TestMigratedSQLiteSessionNativeLineageRoundTrip(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	if _, err := s.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "p-native", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	nativeStarted := now.Add(-time.Minute)
	session := core.AgentSession{ID: "s-native", ProjectID: "p-native", Kind: core.Resumed, Runtime: "codex", Role: "owner", Source: core.SourceResume, SourceRef: "resume", RootID: "s-continued", ContinuationOfID: "s-continued", StartedAt: now, NativeAccessState: core.NativeAccessAvailable, RuntimeHome: "/private/runtime", NativeSessionID: "native-42", NativeSessionRef: "opaque://native/42", NativeSessionStartedAt: &nativeStarted, NativeParentSessionID: "native-41"}
	session.NativeSessionFingerprint = core.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, session.NativeSessionStartedAt)
	if _, _, err := s.Write(ctx, "native-round-trip", "test.write", func(r ports.Repositories) (domain.Result, error) {
		base := core.AgentSession{ID: "s-continued", ProjectID: "p-native", Kind: core.Imported, Runtime: "codex", Role: "owner", Source: core.SourceImport, SourceRef: "import", RootID: "s-continued", StartedAt: now, NativeAccessState: core.NativeAccessUnsupported}
		if err := r.Coordination().CreateSession(ctx, base); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "s-native", Outcome: domain.OutcomeOK}, r.Coordination().CreateSession(ctx, session)
	}); err != nil {
		t.Fatal(err)
	}
	var got core.AgentSession
	if err := s.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		got, ok, err = r.Coordination().GetSession(ctx, session.ID)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("session not found")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.ContinuationOfID != "s-continued" || got.NativeParentSessionID != "native-41" || got.NativeSessionID != "native-42" || got.NativeSessionRef != "opaque://native/42" || got.RuntimeHome != "/private/runtime" || got.NativeSessionFingerprint != session.NativeSessionFingerprint || got.NativeSessionStartedAt == nil || !got.NativeSessionStartedAt.Equal(nativeStarted) {
		t.Fatalf("native session did not round-trip: %+v", got)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO agent_sessions(id,project_id,lineage_kind,runtime,role,instruction_source,source_ref,root_session_id,worktree_ref,native_access_state,native_session_id,started_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", "invalid-native", "p-native", core.Imported, "codex", "owner", core.SourceImport, "import", "invalid-native", "", core.NativeAccessUnsupported, "contradiction", now.Format(time.RFC3339Nano)); err == nil {
		t.Fatal("SQLite accepted unsupported session with a native locator")
	}
}

func TestCoordinationTimestampCorruptionFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		update string
		read   func(*SQLiteStore) (any, bool, error)
		zero   any
	}{
		{"human-created", "UPDATE humans SET created_at='corrupt' WHERE id='h'", func(s *SQLiteStore) (any, bool, error) {
			var v core.Human
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetHuman(context.Background(), "h")
				return err
			})
			return v, ok, err
		}, core.Human{}},
		{"session-started", "UPDATE agent_sessions SET started_at='corrupt' WHERE id='s'", func(s *SQLiteStore) (any, bool, error) {
			var v core.AgentSession
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetSession(context.Background(), "s")
				return err
			})
			return v, ok, err
		}, core.AgentSession{}},
		{"session-native-started", "UPDATE agent_sessions SET native_session_started_at='corrupt' WHERE id='s'", func(s *SQLiteStore) (any, bool, error) {
			var v core.AgentSession
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetSession(context.Background(), "s")
				return err
			})
			return v, ok, err
		}, core.AgentSession{}},
		{"session-ended", "UPDATE agent_sessions SET ended_at='corrupt' WHERE id='s'", func(s *SQLiteStore) (any, bool, error) {
			var v core.AgentSession
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetSession(context.Background(), "s")
				return err
			})
			return v, ok, err
		}, core.AgentSession{}},
		{"session-interrupted", "UPDATE agent_sessions SET interrupted_at='corrupt' WHERE id='s'", func(s *SQLiteStore) (any, bool, error) {
			var v core.AgentSession
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetSession(context.Background(), "s")
				return err
			})
			return v, ok, err
		}, core.AgentSession{}},
		{"session-heartbeat", "UPDATE agent_sessions SET heartbeat_at='corrupt' WHERE id='s'", func(s *SQLiteStore) (any, bool, error) {
			var v core.AgentSession
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetSession(context.Background(), "s")
				return err
			})
			return v, ok, err
		}, core.AgentSession{}},
		{"token-issued", "UPDATE delegation_tokens SET issued_at='corrupt', expires_at='corruptz' WHERE id='token'", func(s *SQLiteStore) (any, bool, error) {
			var v core.DelegationToken
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetToken(context.Background(), "token")
				return err
			})
			return v, ok, err
		}, core.DelegationToken{}},
		{"token-expires", "UPDATE delegation_tokens SET expires_at='corrupt' WHERE id='token'", func(s *SQLiteStore) (any, bool, error) {
			var v core.DelegationToken
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetToken(context.Background(), "token")
				return err
			})
			return v, ok, err
		}, core.DelegationToken{}},
		{"token-revoked", "UPDATE delegation_tokens SET revoked_at='corrupt' WHERE id='token'", func(s *SQLiteStore) (any, bool, error) {
			var v core.DelegationToken
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetToken(context.Background(), "token")
				return err
			})
			return v, ok, err
		}, core.DelegationToken{}},
		{"token-consumed", "UPDATE delegation_tokens SET consumed_at='corrupt', consumed_by_session_id='s' WHERE id='token'", func(s *SQLiteStore) (any, bool, error) {
			var v core.DelegationToken
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetToken(context.Background(), "token")
				return err
			})
			return v, ok, err
		}, core.DelegationToken{}},
		{"task-created", "UPDATE tasks SET created_at='corrupt' WHERE id='t'", func(s *SQLiteStore) (any, bool, error) {
			var v core.Task
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetTask(context.Background(), "t")
				return err
			})
			return v, ok, err
		}, core.Task{}},
		{"task-updated", "UPDATE tasks SET updated_at='corrupt' WHERE id='t'", func(s *SQLiteStore) (any, bool, error) {
			var v core.Task
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetTask(context.Background(), "t")
				return err
			})
			return v, ok, err
		}, core.Task{}},
		{"run-started", "UPDATE task_runs SET started_at='corrupt' WHERE id='run'", func(s *SQLiteStore) (any, bool, error) {
			var v core.TaskRun
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetRun(context.Background(), "run")
				return err
			})
			return v, ok, err
		}, core.TaskRun{}},
		{"run-ended", "UPDATE task_runs SET ended_at='corrupt' WHERE id='run'", func(s *SQLiteStore) (any, bool, error) {
			var v core.TaskRun
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetRun(context.Background(), "run")
				return err
			})
			return v, ok, err
		}, core.TaskRun{}},
		{"run-parent-lost", "UPDATE task_runs SET parent_lost_at='corrupt' WHERE id='run'", func(s *SQLiteStore) (any, bool, error) {
			var v core.TaskRun
			var ok bool
			var err error
			err = s.Read(context.Background(), func(r ports.Repositories) error {
				v, ok, err = r.Coordination().GetRun(context.Background(), "run")
				return err
			})
			return v, ok, err
		}, core.TaskRun{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := corruptTimestampFixture(t)
			if _, err := s.db.Exec(tc.update); err != nil {
				t.Fatal(err)
			}
			got, ok, err := tc.read(s)
			if err == nil || ok || !reflect.DeepEqual(got, tc.zero) {
				t.Fatalf("got=%+v ok=%v err=%v; want zero entity, false, parse error", got, ok, err)
			}
		})
	}
	t.Run("token-list", func(t *testing.T) {
		s := corruptTimestampFixture(t)
		if _, err := s.db.Exec("UPDATE delegation_tokens SET issued_at='corrupt', expires_at='corruptz' WHERE id='token'"); err != nil {
			t.Fatal(err)
		}
		var got []core.DelegationToken
		err := s.Read(context.Background(), func(r ports.Repositories) error {
			var err error
			got, err = r.Coordination().FindTokenByVerifier(context.Background(), "p", "t", "s")
			return err
		})
		if err == nil || got != nil {
			t.Fatalf("got=%+v err=%v; want nil list and parse error", got, err)
		}
	})
}

func corruptTimestampFixture(t *testing.T) *SQLiteStore {
	t.Helper()
	s := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	if _, err := s.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "p", stamp(now)); err != nil {
		t.Fatal(err)
	}
	nativeStarted, ended, interrupted, heartbeat := now.Add(-time.Minute), now.Add(time.Minute), now.Add(2*time.Minute), now.Add(3*time.Minute)
	if _, _, err := s.Write(ctx, "corrupt-fixture", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		if err := c.CreateHuman(ctx, core.Human{ID: "h", DisplayName: "human", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		session := core.AgentSession{ID: "s", ProjectID: "p", HumanID: "h", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "s", StartedAt: now, EndedAt: &ended, InterruptedAt: &interrupted, HeartbeatAt: &heartbeat, NativeAccessState: core.NativeAccessAvailable, NativeSessionID: "native", NativeSessionRef: "native-ref", NativeSessionStartedAt: &nativeStarted}
		session.NativeSessionFingerprint = core.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, session.NativeSessionStartedAt)
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		if _, err := c.CreateTask(ctx, core.Task{ID: "t", ProjectID: "p", Title: "task", State: core.TaskReady, CreatedAt: now, UpdatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.IssueToken(ctx, core.DelegationToken{ID: "token", ProjectID: "p", TaskID: "t", ParentSessionID: "s", Algorithm: "PBKDF2-HMAC-SHA256", Iterations: 100000, Salt: make([]byte, 16), Verifier: make([]byte, 32), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "run", TaskID: "t", SessionID: "s", State: core.RunRunning, StartedAt: now, EndedAt: &ended, ParentLostAt: &interrupted}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "fixture", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TRIGGER humans_no_update; DROP TRIGGER agent_sessions_no_update"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestListTasksScopesProjectAndFailsClosedOnCorruption(t *testing.T) {
	s := corruptTimestampFixture(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "other", stamp(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO tasks(id,project_id,display_number,title,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", "other-task", "other", 1, "other", core.TaskReady, stamp(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC)), stamp(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	var tasks []core.Task
	if err := s.Read(ctx, func(r ports.Repositories) error {
		var err error
		tasks, err = r.Coordination().ListTasks(ctx, "p")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t" {
		t.Fatalf("project list leaked rows: %#v", tasks)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE tasks SET created_at='corrupt' WHERE id='t'"); err != nil {
		t.Fatal(err)
	}
	err := s.Read(ctx, func(r ports.Repositories) error { _, err := r.Coordination().ListTasks(ctx, "p"); return err })
	if err == nil {
		t.Fatal("corrupt task row was accepted")
	}
}

func TestListTasksRejectsInvalidTaskFields(t *testing.T) {
	for _, update := range []string{"UPDATE tasks SET state='INVALID' WHERE id='t'", "UPDATE tasks SET title='' WHERE id='t'", "UPDATE tasks SET display_number=0 WHERE id='t'"} {
		s := corruptTimestampFixture(t)
		if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(update); err != nil {
			t.Fatal(err)
		}
		err := s.Read(context.Background(), func(r ports.Repositories) error {
			_, err := r.Coordination().ListTasks(context.Background(), "p")
			return err
		})
		if err == nil {
			t.Fatalf("invalid row accepted after %s", update)
		}
	}
}

func TestScopedCoordinationRejectsCrossProjectTaskAndRunAccess(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, project := range []string{"project-a", "project-b"} {
		if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", project, stamp(now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.Write(ctx, "cross-project-fixture", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, project := range []string{"project-a", "project-b"} {
			sessionID := core.ID("session-" + project)
			taskID := core.ID("task-" + project)
			if err := c.CreateSession(ctx, core.AgentSession{ID: sessionID, ProjectID: core.ID(project), Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: sessionID, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if _, err := c.CreateTask(ctx, core.Task{ID: taskID, ProjectID: core.ID(project), Title: project, State: core.TaskReady, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			otherTaskID := core.ID("task-" + project + "-other")
			if _, err := c.CreateTask(ctx, core.Task{ID: otherTaskID, ProjectID: core.ID(project), Title: project + " other", State: core.TaskReady, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateRun(ctx, core.TaskRun{ID: core.ID("run-" + project), TaskID: taskID, SessionID: sessionID, State: core.RunRunning, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateProgress(ctx, coord.Progress{ID: "progress-" + project, TaskID: string(taskID), RunID: "run-" + project, SessionID: string(sessionID), Phase: coord.PhaseImplement, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateDependency(ctx, coord.Dependency{ID: "dependency-" + project, PrerequisiteTaskID: string(taskID), DependentTaskID: string(otherTaskID), Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}, now); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateMessage(ctx, project, coord.MailMessage{ID: "message-" + project, ThreadID: "thread-" + project, SenderSessionID: string(sessionID), RelatedTaskID: string(taskID), Type: coord.MessageNotice, Recipients: []coord.RecipientTarget{{SessionID: string(sessionID)}}, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateHandoff(ctx, coord.Handoff{ID: "handoff-" + project, TaskID: string(taskID), RunID: "run-" + project, SourceSessionID: string(sessionID), Summary: "handoff", Status: coord.HandoffSubmitted, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateHandoffDecision(ctx, coord.HandoffDecision{ID: "decision-" + project, HandoffID: "handoff-" + project, Decision: coord.HandoffAccepted, DecidedBySessionID: string(sessionID), CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateAdoption(ctx, coord.Adoption{ID: "adoption-" + project, ProjectID: project, NewOwnerSessionID: string(sessionID), TaskID: string(otherTaskID), Reason: "orphan", CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "fixture", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	scoped := store.Scope("project-a")
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		c := r.Coordination()
		if _, ok, err := c.GetSession(ctx, "session-project-b"); err != nil || ok {
			t.Fatalf("foreign session visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetTask(ctx, "task-project-b"); err != nil || ok {
			t.Fatalf("foreign task visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetRun(ctx, "run-project-b"); err != nil || ok {
			t.Fatalf("foreign run visible: ok=%t err=%v", ok, err)
		}
		if progress, err := c.ListProgress(ctx, "task-project-b"); err != nil || len(progress) != 0 {
			t.Fatalf("foreign progress visible: %#v err=%v", progress, err)
		}
		if _, ok, err := c.GetProgress(ctx, "progress-project-b"); err != nil || ok {
			t.Fatalf("foreign progress visible by id: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetDependency(ctx, "dependency-project-b"); err != nil || ok {
			t.Fatalf("foreign dependency visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetMessage(ctx, "message-project-b"); err != nil || ok {
			t.Fatalf("foreign message visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetDeliveryByID(ctx, "message-project-b:0"); err != nil || ok {
			t.Fatalf("foreign delivery visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetHandoff(ctx, "handoff-project-b"); err != nil || ok {
			t.Fatalf("foreign handoff visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetHandoffDecisionByID(ctx, "decision-project-b"); err != nil || ok {
			t.Fatalf("foreign handoff decision visible: ok=%t err=%v", ok, err)
		}
		if _, ok, err := c.GetAdoptionByID(ctx, "adoption-project-b"); err != nil || ok {
			t.Fatalf("foreign adoption visible: ok=%t err=%v", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "cross-project-claim", "test.write", func(r ports.Repositories) (domain.Result, error) {
		_, won, err := r.Coordination().ClaimTask(ctx, "task-project-b", "session-project-a", now)
		if err != nil {
			return domain.Result{}, err
		}
		if won {
			return domain.Result{}, errors.New("foreign task claim succeeded")
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "cross-project-run", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateRun(ctx, core.TaskRun{ID: "cross-project-run", TaskID: "task-project-b", SessionID: "session-project-a", State: core.RunRunning, StartedAt: now})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("cross-project run creation succeeded")
	}
	if _, _, err := scoped.Write(ctx, "cross-project-p3b", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		if err := c.CreateProgress(ctx, coord.Progress{ID: "cross-progress", TaskID: "task-project-b", RunID: "run-project-b", SessionID: "session-project-b", Phase: coord.PhaseImplement, CreatedAt: now}); err == nil {
			return domain.Result{}, errors.New("foreign progress creation succeeded")
		}
		if err := c.CreateDependency(ctx, coord.Dependency{ID: "cross-dependency", PrerequisiteTaskID: "task-project-a", DependentTaskID: "task-project-b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}, now); err == nil {
			return domain.Result{}, errors.New("cross-project dependency creation succeeded")
		}
		if err := c.CreateMessage(ctx, "project-b", coord.MailMessage{ID: "cross-message", ThreadID: "cross-thread", SenderSessionID: "session-project-b", Type: coord.MessageNotice, Recipients: []coord.RecipientTarget{{SessionID: "session-project-b"}}, CreatedAt: now}); err == nil {
			return domain.Result{}, errors.New("foreign message creation succeeded")
		}
		if err := c.CreateHandoff(ctx, coord.Handoff{ID: "cross-handoff", TaskID: "task-project-b", RunID: "run-project-b", SourceSessionID: "session-project-b", Summary: "handoff", Status: coord.HandoffSubmitted, CreatedAt: now}); err == nil {
			return domain.Result{}, errors.New("foreign handoff creation succeeded")
		}
		if err := c.CreateAdoption(ctx, coord.Adoption{ID: "cross-adoption", ProjectID: "project-b", NewOwnerSessionID: "session-project-b", TaskID: "task-project-b", Reason: "orphan", CreatedAt: now}); err == nil {
			return domain.Result{}, errors.New("foreign adoption creation succeeded")
		}
		if ok, err := c.MarkDependencySatisfied(ctx, "dependency-project-b", now, nil, "message-project-b"); err != nil || ok {
			return domain.Result{}, errors.New("foreign dependency mutation succeeded")
		}
		if err := c.SetDelivery(ctx, coord.RecipientDelivery{MessageID: "message-project-b", Recipient: coord.RecipientTarget{SessionID: "session-project-b"}, DeliveredAt: now}); err == nil {
			return domain.Result{}, errors.New("foreign delivery mutation succeeded")
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScopedCoordinationRejectsForeignSessionCreationReferences(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, project := range []string{"project-a", "project-b"} {
		if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", project, stamp(now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.Write(ctx, "foreign-session-fixture", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, project := range []string{"project-a", "project-b"} {
			sessionID := core.ID("session-" + project)
			if err := c.CreateSession(ctx, core.AgentSession{ID: sessionID, ProjectID: core.ID(project), Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: sessionID, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if _, err := c.CreateTask(ctx, core.Task{ID: core.ID("task-" + project), ProjectID: core.ID(project), Title: project, State: core.TaskReady, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	scoped := store.Scope("project-a")
	if _, _, err := scoped.Write(ctx, "foreign-session-references", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		sameProject := core.AgentSession{ID: "session-a-child", ProjectID: "project-a", Kind: core.AgentDelegated, Runtime: "test", Role: "worker", Source: core.SourceDelegationToken, SourceRef: "fixture", ParentID: "session-project-a", RootID: "session-project-a", ContinuationOfID: "session-project-a", TaskID: "task-project-a", StartedAt: now}
		if err := c.CreateSession(ctx, sameProject); err != nil {
			return domain.Result{}, err
		}
		for _, reference := range []struct {
			name  string
			apply func(*core.AgentSession)
		}{
			{"parent", func(s *core.AgentSession) { s.ParentID = "session-project-b" }},
			{"root", func(s *core.AgentSession) { s.RootID = "session-project-b" }},
			{"continuation", func(s *core.AgentSession) { s.ContinuationOfID = "session-project-b" }},
			{"task", func(s *core.AgentSession) { s.TaskID = "task-project-b" }},
		} {
			session := sameProject
			session.ID = core.ID("foreign-session-" + reference.name)
			reference.apply(&session)
			if err := c.CreateSession(ctx, session); err == nil {
				return domain.Result{}, errors.New("foreign " + reference.name + " session reference was accepted")
			}
			if _, ok, err := c.GetSession(ctx, session.ID); err != nil || ok {
				return domain.Result{}, errors.New("foreign session reference published a session")
			}
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScopedCoordinationRejectsForeignTaskCreationReferencesWithoutAllocatingNumbers(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, project := range []string{"project-a", "project-b"} {
		if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", project, stamp(now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.Write(ctx, "foreign-task-fixture", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, project := range []string{"project-a", "project-b"} {
			sessionID := core.ID("session-" + project)
			if err := c.CreateSession(ctx, core.AgentSession{ID: sessionID, ProjectID: core.ID(project), Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: sessionID, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if _, err := c.CreateTask(ctx, core.Task{ID: core.ID("task-" + project), ProjectID: core.ID(project), Title: project, State: core.TaskReady, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	scoped := store.Scope("project-a")
	if _, _, err := scoped.Write(ctx, "foreign-task-references", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, reference := range []struct {
			name  string
			apply func(*core.Task)
		}{
			{"creator", func(task *core.Task) { task.CreatedBySessionID = "session-project-b" }},
			{"parent", func(task *core.Task) { task.ParentTaskID = "task-project-b" }},
			{"supersedes", func(task *core.Task) { task.Supersedes = "task-project-b" }},
		} {
			task := core.Task{ID: core.ID("foreign-task-" + reference.name), ProjectID: "project-a", Title: reference.name, State: core.TaskReady, CreatedAt: now, UpdatedAt: now}
			reference.apply(&task)
			if _, err := c.CreateTask(ctx, task); err == nil {
				return domain.Result{}, errors.New("foreign " + reference.name + " task reference was accepted")
			}
			if _, ok, err := c.GetTask(ctx, task.ID); err != nil || ok {
				return domain.Result{}, errors.New("foreign task reference published a task")
			}
		}
		task, err := c.CreateTask(ctx, core.Task{ID: "task-a-child", ProjectID: "project-a", Title: "same project", State: core.TaskReady, CreatedBySessionID: "session-project-a", ParentTaskID: "task-project-a", Supersedes: "task-project-a", CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return domain.Result{}, err
		}
		if task.DisplayNumber != 2 {
			return domain.Result{}, errors.New("failed task creation consumed a display number")
		}
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScopedHumansRejectForeignReadsAndReferencesWithoutMutation(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, project := range []string{"human-project-a", "human-project-b"} {
		if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", project, stamp(now)); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct {
		project string
		id      core.ID
	}{
		{"human-project-a", "human-a"},
		{"human-project-b", "human-b"},
	} {
		scoped := store.Scope(domain.ProjectID(fixture.project))
		if _, _, err := scoped.Write(ctx, domain.IdempotencyKey("create-"+string(fixture.id)), "test.write", func(r ports.Repositories) (domain.Result, error) {
			err := r.Coordination().CreateHuman(ctx, core.Human{ID: fixture.id, ProjectID: core.ID(fixture.project), DisplayName: string(fixture.id), Confidence: core.ConfidenceExplicit, CreatedAt: now})
			return domain.Result{ID: domain.ResultID(fixture.id), Outcome: domain.OutcomeOK}, err
		}); err != nil {
			t.Fatal(err)
		}
	}
	scoped := store.Scope("human-project-a")
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		human, ok, err := r.Coordination().GetHuman(ctx, "human-a")
		if err != nil || !ok || human.ProjectID != "human-project-a" {
			return fmt.Errorf("same-project human unavailable: human=%+v ok=%t err=%w", human, ok, err)
		}
		if _, ok, err := r.Coordination().GetHuman(ctx, "human-b"); err != nil || ok {
			return fmt.Errorf("foreign human visible: ok=%t err=%w", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "foreign-human-supersession", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateHuman(ctx, core.Human{ID: "human-a-foreign-successor", ProjectID: "human-project-a", DisplayName: "foreign successor", Confidence: core.ConfidenceExplicit, CreatedAt: now, Supersedes: "human-b"})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("foreign human supersession was created")
	}
	if _, _, err := scoped.Write(ctx, "same-project-human-supersession", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateHuman(ctx, core.Human{ID: "human-a-successor", ProjectID: "human-project-a", DisplayName: "same-project successor", Confidence: core.ConfidenceExplicit, CreatedAt: now, Supersedes: "human-a"})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatalf("same-project human supersession rejected: %v", err)
	}
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		if _, ok, err := r.Coordination().GetHuman(ctx, "human-a-foreign-successor"); err != nil || ok {
			return fmt.Errorf("foreign human supersession persisted: ok=%t err=%w", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "foreign-human-session", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateSession(ctx, core.AgentSession{ID: "foreign-human-session", ProjectID: "human-project-a", HumanID: "human-b", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "foreign-human-session", StartedAt: now})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("foreign human session was created")
	}
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		if _, ok, err := r.Coordination().GetSession(ctx, "foreign-human-session"); err != nil || ok {
			return fmt.Errorf("foreign human session persisted: ok=%t err=%w", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "same-human-session", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateSession(ctx, core.AgentSession{ID: "same-human-session", ProjectID: "human-project-a", HumanID: "human-a", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "same-human-session", StartedAt: now})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "foreign-human-message", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().CreateMessage(ctx, "human-project-a", coord.MailMessage{ID: "foreign-human-message", ThreadID: "thread", SenderSessionID: "same-human-session", Type: coord.MessageNotice, Subject: "subject", Body: "body", Recipients: []coord.RecipientTarget{{HumanID: "human-b"}}, CreatedAt: now})
		return domain.Result{Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("foreign human message was created")
	}
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		if _, ok, err := r.Coordination().GetMessage(ctx, "foreign-human-message"); err != nil || ok {
			return fmt.Errorf("foreign human message persisted: ok=%t err=%w", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatProjectionAndRunLifecycle(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "liveness-project", stamp(now)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "seed-liveness", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		session := core.AgentSession{ID: "liveness-session", ProjectID: "liveness-project", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "liveness-session", StartedAt: now}
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		task, err := c.CreateTask(ctx, core.Task{ID: "liveness-task", ProjectID: "liveness-project", Title: "liveness", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "liveness-run", TaskID: task.ID, SessionID: session.ID, State: core.RunRunning, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "liveness-task", Outcome: domain.OutcomeOK}, c.CreateRun(ctx, core.TaskRun{ID: "parent-loss-run", TaskID: task.ID, SessionID: session.ID, State: core.RunRunning, StartedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	heartbeatAt := now.Add(time.Minute)
	if _, _, err := store.Write(ctx, "stale-heartbeat", "test.write", func(r ports.Repositories) (domain.Result, error) {
		if err := r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "liveness-heartbeat", SessionID: "liveness-session", ObservedAt: heartbeatAt, Liveness: core.Stale, Detail: []byte("{}")}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "liveness-session", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Read(ctx, func(r ports.Repositories) error {
		session, ok, err := r.Coordination().GetSession(ctx, "liveness-session")
		if err != nil || !ok || session.Liveness != core.Stale || session.HeartbeatAt == nil || !session.HeartbeatAt.Equal(heartbeatAt) || session.InterruptedAt != nil {
			t.Fatalf("projected session=%+v ok=%t err=%v", session, ok, err)
		}
		sessions, err := r.Coordination().ListSessions(ctx, "liveness-project")
		if err != nil || len(sessions) != 1 || sessions[0].Liveness != core.Stale || sessions[0].HeartbeatAt == nil || !sessions[0].HeartbeatAt.Equal(heartbeatAt) {
			t.Fatalf("projected list=%+v err=%v", sessions, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "alive-after-stale", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "alive-heartbeat", SessionID: "liveness-session", ObservedAt: heartbeatAt.Add(time.Minute), Liveness: core.Alive, Detail: []byte("{}")})
		return domain.Result{ID: "liveness-session", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Read(ctx, func(r ports.Repositories) error {
		session, ok, err := r.Coordination().GetSession(ctx, "liveness-session")
		if err != nil || !ok || session.Liveness != core.Alive {
			t.Fatalf("stale session did not recover after alive heartbeat: %+v ok=%t err=%v", session, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "pause-run", "test.write", func(r ports.Repositories) (domain.Result, error) {
		run, err := r.Coordination().TransitionRun(ctx, "liveness-run", core.RunWaiting, nil, heartbeatAt)
		if err != nil {
			return domain.Result{}, err
		}
		if run.EndedAt != nil {
			t.Fatalf("waiting run ended at %v", run.EndedAt)
		}
		run, err = r.Coordination().TransitionRun(ctx, "liveness-run", core.RunRunning, nil, heartbeatAt.Add(time.Minute))
		if err != nil {
			return domain.Result{}, err
		}
		if run.EndedAt != nil {
			t.Fatalf("resumed run ended at %v", run.EndedAt)
		}
		run, err = r.Coordination().TransitionRun(ctx, "liveness-run", core.RunWorkComplete, nil, heartbeatAt.Add(2*time.Minute))
		if err != nil {
			return domain.Result{}, err
		}
		if run.EndedAt == nil || !run.EndedAt.Equal(heartbeatAt.Add(2*time.Minute)) {
			t.Fatalf("completed run ended at %v", run.EndedAt)
		}
		lostAt := heartbeatAt.Add(3 * time.Minute)
		lost, err := r.Coordination().RecordParentLoss(ctx, "parent-loss-run", core.Heartbeat{ID: "parent-loss-heartbeat", SessionID: "liveness-session", ObservedAt: lostAt, Liveness: core.Interrupted, Detail: []byte("{}")})
		if err != nil {
			return domain.Result{}, err
		}
		if lost.ParentLostAt == nil || !lost.ParentLostAt.Equal(lostAt) || lost.EndedAt == nil || !lost.EndedAt.Equal(lostAt) || lost.State != core.RunInterrupted {
			t.Fatalf("parent loss was not durably projected: %+v", lost)
		}
		return domain.Result{ID: "liveness-run", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedSessionHeartbeatIsTerminal(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "interrupted-project", stamp(now)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "seed-interrupted-session", "test.write", func(r ports.Repositories) (domain.Result, error) {
		session := core.AgentSession{ID: "interrupted-session", ProjectID: "interrupted-project", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "interrupted-session", StartedAt: now}
		if err := r.Coordination().CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "interrupted-session", Outcome: domain.OutcomeOK}, r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "interrupted", SessionID: session.ID, ObservedAt: now, Liveness: core.Interrupted, Detail: []byte("{}")})
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "revive-interrupted-session", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "revival", SessionID: "interrupted-session", ObservedAt: now.Add(time.Minute), Liveness: core.Alive, Detail: []byte("{}")})
		return domain.Result{ID: "interrupted-session", Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("revived interrupted session")
	}
	var heartbeats int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM session_heartbeats").Scan(&heartbeats); err != nil {
		t.Fatal(err)
	}
	if heartbeats != 1 {
		t.Fatalf("revival mutated heartbeats: got=%d want=1", heartbeats)
	}
	if err := store.Read(ctx, func(r ports.Repositories) error {
		session, ok, err := r.Coordination().GetSession(ctx, "interrupted-session")
		if err != nil || !ok || session.Liveness != core.Interrupted || session.InterruptedAt == nil || !session.InterruptedAt.Equal(now) {
			t.Fatalf("interrupted projection=%+v ok=%t err=%v", session, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordParentLossInterruptsMultipleOwnedRunsWithCanonicalTimestamp(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "multi-parent-loss-project", stamp(now)); err != nil {
		t.Fatal(err)
	}
	lostAt := now.Add(time.Minute)
	if _, _, err := store.Write(ctx, "mark-multiple-parent-loss-runs", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		session := core.AgentSession{ID: "multi-parent-loss-session", ProjectID: "multi-parent-loss-project", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "multi-parent-loss-session", StartedAt: now}
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		task, err := c.CreateTask(ctx, core.Task{ID: "multi-parent-loss-task", ProjectID: "multi-parent-loss-project", Title: "multiple parent loss", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return domain.Result{}, err
		}
		for _, id := range []core.ID{"multi-parent-loss-run-one", "multi-parent-loss-run-two"} {
			if err := c.CreateRun(ctx, core.TaskRun{ID: id, TaskID: task.ID, SessionID: session.ID, State: core.RunRunning, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
		}
		first, err := c.RecordParentLoss(ctx, "multi-parent-loss-run-one", core.Heartbeat{ID: "multi-parent-loss-heartbeat-one", SessionID: session.ID, ObservedAt: lostAt, Liveness: core.Interrupted, Detail: []byte("{}")})
		if err != nil {
			return domain.Result{}, err
		}
		second, err := c.RecordParentLoss(ctx, "multi-parent-loss-run-two", core.Heartbeat{ID: "multi-parent-loss-heartbeat-two", SessionID: session.ID, ObservedAt: lostAt.Add(time.Minute), Liveness: core.Interrupted, Detail: []byte("{}")})
		if err != nil {
			return domain.Result{}, err
		}
		for _, run := range []core.TaskRun{first, second} {
			if run.State != core.RunInterrupted || run.ParentLostAt == nil || !run.ParentLostAt.Equal(lostAt) || run.EndedAt == nil || !run.EndedAt.Equal(lostAt) {
				t.Fatalf("parent loss run=%+v want interrupted at %v", run, lostAt)
			}
		}
		return domain.Result{ID: domain.ResultID(task.ID), Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Read(ctx, func(r ports.Repositories) error {
		for _, id := range []core.ID{"multi-parent-loss-run-one", "multi-parent-loss-run-two"} {
			run, ok, err := r.Coordination().GetRun(ctx, id)
			if err != nil || !ok || run.State != core.RunInterrupted || run.ParentLostAt == nil || !run.ParentLostAt.Equal(lostAt) || run.EndedAt == nil || !run.EndedAt.Equal(lostAt) {
				t.Fatalf("persisted parent loss run=%+v ok=%t err=%v", run, ok, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
