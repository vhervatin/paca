package e2e_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Paca-AI/api/internal/worker"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// startWorkflowConsumer wires up and starts a real worker.WorkflowConsumer
// against the shared Valkey stream, so that task status changes made through
// the HTTP API actually get evaluated against active workflows — exercising
// the automation *engine*, not just the workflow CRUD surface. It is stopped
// automatically at test cleanup.
//
// This is deliberately NOT part of newE2EEnv: only the tests in this file pay
// the cost of running a consumer (Stop() can block for up to a few seconds
// draining its in-flight blocking read), so unrelated e2e tests are unaffected.
func startWorkflowConsumer(t *testing.T, env *e2eEnv) *worker.WorkflowConsumer {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	wc := worker.NewWorkflowConsumer(env.redisClient, env.workflowRepo, env.taskRepo, env.taskSvc, env.activitySvc, nil, log)
	wc.Start(env.ctx)
	t.Cleanup(wc.Stop)
	return wc
}

// getTaskViaAPI fetches a task and returns its decoded response body.
func getTaskViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID string) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projectID, taskID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	return assertDataMap(t, env2)
}

// setTaskStatusViaAPI changes a task's status through the normal task-update
// endpoint — the same path a human PATCH would take, and the one the
// automation engine listens for.
func setTaskStatusViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID, statusID string) {
	t.Helper()
	body := jsonBody(t, map[string]any{"status_id": statusID})
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projectID, taskID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

// setStatusRuleViaAPI creates or updates a workflow's status->assignee rule.
func setStatusRuleViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, workflowID, statusID, assigneeMemberID string) {
	t.Helper()
	body := jsonBody(t, map[string]any{"status_id": statusID, "assignee_member_id": assigneeMemberID})
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/workflows/%s/status-rules", env.base, projectID, workflowID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
}

// waitForTaskAssignee polls the task until its assignee_ids is exactly
// [wantAssigneeID] or timeout elapses. Reassignment happens asynchronously
// (via a Valkey-stream event the WorkflowConsumer processes in the
// background), so tests must poll rather than assert immediately after the
// triggering PATCH. A status rule firing replaces the task's entire assignee
// set with its single configured member, so "assigned to X" means the array
// is exactly [X], not merely contains it.
//
// The timeout is generous because the consumer group is shared across the
// whole e2e suite: the first automation test to run drains any backlog of
// task.updated events produced by earlier, unrelated tests (harmless no-ops,
// since those tasks live in different per-test databases) before it reaches
// this test's own event.
func waitForTaskAssignee(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID, wantAssigneeID string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var data map[string]any
	var lastAssignees []any
	for time.Now().Before(deadline) {
		data = getTaskViaAPI(t, env, client, token, projectID, taskID)
		lastAssignees, _ = data["assignee_ids"].([]any)
		if len(lastAssignees) == 1 && lastAssignees[0] == wantAssigneeID {
			return data
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for task %s to be assigned to %q; last observed assignee_ids=%v", taskID, wantAssigneeID, lastAssignees)
	return nil
}

// addProjectMemberWithWorkflowPerms seeds a new human user, adds them to
// projectID with a role that can read/write tasks and workflows, and returns
// their project_members.id.
func addProjectMemberWithWorkflowPerms(t *testing.T, env *e2eEnv, ownerClient *http.Client, ownerToken, projectID, username, password string) string {
	t.Helper()
	seedUser(t, env, username, password, username)
	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	roleID := createProjectRoleWithPermsViaAPI(t, env, ownerClient, ownerToken, projectID, "editor-"+uuid.NewString(),
		map[string]any{"projects.read": true, "tasks.read": true, "tasks.write": true, "workflows.read": true, "workflows.write": true})
	addMemberViaAPI(t, env, ownerClient, ownerToken, projectID, user.ID.String(), roleID)

	members := listProjectMembersViaAPI(t, env, ownerClient, ownerToken, projectID)
	memberID := memberIDForUser(members, user.ID.String())
	if memberID == "" {
		t.Fatalf("expected user %q to resolve to a project member", username)
	}
	return memberID
}

// ---------------------------------------------------------------------------
// Event 1: a task's own status change is looked up in the workflow's rules.
// ---------------------------------------------------------------------------

func TestE2EWorkflowAutomation_StatusChangeReassignsTask(t *testing.T) {
	env := newE2EEnv(t)
	startWorkflowConsumer(t, env)

	ownerUsername := "workflow-auto-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "workflowautoowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "workflowautoowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	// A second real member, distinct from the auto-seeded default assignee
	// (the workflow creator), so a successful reassignment is unambiguous.
	secondMemberID := addProjectMemberWithWorkflowPerms(t, env, ownerClient, ownerToken, projID,
		"workflow-auto-member-"+uuid.NewString(), "workflowautomember1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Automated Task")
	workflowID := createWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, "Status Rule Automation")
	addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, task)

	// Explicit rule: reaching "In Progress" reassigns to the second member.
	setStatusRuleViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, inProgressID, secondMemberID)

	activateWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, workflowID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForTaskAssignee(t, env, ownerClient, ownerToken, projID, task, secondMemberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Event 2: once a predecessor reaches the workflow's derived done status, its
// successor is reassigned using the successor's OWN current status — without
// changing the successor's status.
// ---------------------------------------------------------------------------

func TestE2EWorkflowAutomation_TaskDoneCascadesAssignmentToSuccessor(t *testing.T) {
	env := newE2EEnv(t)
	startWorkflowConsumer(t, env)

	ownerUsername := "workflow-cascade-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "workflowcascadeowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "workflowcascadeowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	member2ID := addProjectMemberWithWorkflowPerms(t, env, ownerClient, ownerToken, projID,
		"workflow-cascade-m2-"+uuid.NewString(), "workflowcascadem2pass")
	member3ID := addProjectMemberWithWorkflowPerms(t, env, ownerClient, ownerToken, projID,
		"workflow-cascade-m3-"+uuid.NewString(), "workflowcascadem3pass")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	backlogID := statusIDByName(statuses, "Backlog")
	doneID := statusIDByName(statuses, "Done")

	taskA := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor Task")
	taskB := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Successor Task")

	workflowID := createWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, "Cascade Workflow")
	nodeAData := addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, taskA)
	nodeBData := addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, taskB)
	nodeAID, _ := nodeAData["id"].(string)
	nodeBID, _ := nodeBData["id"].(string)
	addWorkflowEdgeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, nodeAID, nodeBID)

	// Backlog (taskB's current status, untouched) reassigns to member2;
	// Done reassigns whichever task just finished to member3.
	setStatusRuleViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, backlogID, member2ID)
	setStatusRuleViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, doneID, member3ID)

	activateWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, workflowID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskA, doneID)

	// Event 1: taskA itself reaches Done -> reassigned per the Done rule.
	waitForTaskAssignee(t, env, ownerClient, ownerToken, projID, taskA, member3ID, 20*time.Second)

	// Event 2: taskA (predecessor) done -> taskB (successor) reassigned using
	// ITS OWN current status (Backlog) against the same rules.
	dataB := waitForTaskAssignee(t, env, ownerClient, ownerToken, projID, taskB, member2ID, 20*time.Second)
	if statusID, _ := dataB["status_id"].(string); statusID != backlogID {
		t.Errorf("expected the successor's status to remain Backlog (only its assignment should change), got %q", statusID)
	}
}

// ---------------------------------------------------------------------------
// AND-join: a successor with multiple predecessors is only reassigned once
// ALL of its predecessors have reached done.
// ---------------------------------------------------------------------------

func TestE2EWorkflowAutomation_AndJoinWaitsForAllPredecessors(t *testing.T) {
	env := newE2EEnv(t)
	startWorkflowConsumer(t, env)

	ownerUsername := "workflow-andjoin-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "workflowandjoinowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "workflowandjoinowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	ownerUser, err := env.userRepo.FindByUsername(env.ctx, ownerUsername)
	if err != nil {
		t.Fatalf("find owner user: %v", err)
	}

	secondMemberID := addProjectMemberWithWorkflowPerms(t, env, ownerClient, ownerToken, projID,
		"workflow-andjoin-member-"+uuid.NewString(), "workflowandjoinmemberpass")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	backlogID := statusIDByName(statuses, "Backlog")
	doneID := statusIDByName(statuses, "Done")

	taskA := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor A")
	taskC := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor C")
	taskB := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Joint Successor")

	workflowID := createWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, "AND-Join Workflow")
	nodeAData := addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, taskA)
	nodeCData := addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, taskC)
	nodeBData := addWorkflowNodeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, taskB)
	nodeAID, _ := nodeAData["id"].(string)
	nodeCID, _ := nodeCData["id"].(string)
	nodeBID, _ := nodeBData["id"].(string)
	addWorkflowEdgeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, nodeAID, nodeBID)
	addWorkflowEdgeViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, nodeCID, nodeBID)

	// Backlog (taskB's current status) reassigns to the second member once
	// the AND-join is satisfied by both predecessors.
	setStatusRuleViaAPI(t, env, ownerClient, ownerToken, projID, workflowID, backlogID, secondMemberID)

	activateWorkflowViaAPI(t, env, ownerClient, ownerToken, projID, workflowID)

	members := listProjectMembersViaAPI(t, env, ownerClient, ownerToken, projID)
	ownerMemberID := memberIDForUser(members, ownerUser.ID.String())

	t.Run("single_predecessor_done_does_not_fire_the_join", func(t *testing.T) {
		setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskA, doneID)

		// Synchronize on taskA's own reassignment (its Done rule falls back to
		// the auto-seeded default: the workflow's creator) to know the
		// consumer has finished processing this event, including the
		// AND-join evaluation for taskB, which runs synchronously within the
		// same handler call.
		waitForTaskAssignee(t, env, ownerClient, ownerToken, projID, taskA, ownerMemberID, 20*time.Second)

		dataB := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, taskB)
		if assignees, ok := dataB["assignee_ids"].([]any); ok && len(assignees) > 0 {
			t.Errorf("expected the joint successor to remain unassigned until ALL predecessors are done, got assignee_ids=%v", assignees)
		}
	})

	t.Run("second_predecessor_done_fires_the_join", func(t *testing.T) {
		setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskC, doneID)
		waitForTaskAssignee(t, env, ownerClient, ownerToken, projID, taskB, secondMemberID, 20*time.Second)
	})
}
