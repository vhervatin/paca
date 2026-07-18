package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	workflowdom "github.com/Paca-AI/api/internal/domain/workflow"
	"github.com/google/uuid"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// isAssignedOnlyTo reports whether t's assignee set is exactly {member} —
// the shape a status rule's replace-all-assignees reassignment produces.
func isAssignedOnlyTo(t *taskdom.Task, member uuid.UUID) bool {
	return len(t.AssigneeIDs) == 1 && t.AssigneeIDs[0] == member
}

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeGraphStore struct {
	workflows   map[uuid.UUID]*workflowdom.Workflow
	nodes       map[uuid.UUID]*workflowdom.Node
	rules       map[uuid.UUID][]*workflowdom.StatusRule       // keyed by workflow ID
	transitions map[uuid.UUID][]*workflowdom.StatusTransition // keyed by workflow ID
	edges       []*workflowdom.Edge

	transitionsCalls int // counts real ListStatusTransitionsByWorkflow calls, to assert evalCache hits
	rulesCalls       int // counts real ListStatusRulesByWorkflow calls, to prove rules are re-read fresh (not cached) across an event's fan-out

	// afterListStatusRulesCall, if non-nil, runs synchronously right after
	// each ListStatusRulesByWorkflow call returns, receiving the 1-based
	// call count — used to simulate a concurrent SetStatusRule edit landing
	// mid-fan-out (between two nodes of the same event evaluating rules).
	afterListStatusRulesCall func(call int)

	findWorkflowCalls int
	// archiveAfterFindWorkflowCall, if non-zero, flips every workflow to
	// archived right after the Nth FindWorkflowByID call returns — used to
	// simulate a concurrent archive landing mid-way through one event's
	// node/edge fan-out.
	archiveAfterFindWorkflowCall int
}

func newFakeGraphStore() *fakeGraphStore {
	return &fakeGraphStore{
		workflows:   make(map[uuid.UUID]*workflowdom.Workflow),
		nodes:       make(map[uuid.UUID]*workflowdom.Node),
		rules:       make(map[uuid.UUID][]*workflowdom.StatusRule),
		transitions: make(map[uuid.UUID][]*workflowdom.StatusTransition),
	}
}

func (f *fakeGraphStore) FindWorkflowByID(_ context.Context, id uuid.UUID) (*workflowdom.Workflow, error) {
	w, ok := f.workflows[id]
	if !ok {
		return nil, workflowdom.ErrNotFound
	}
	f.findWorkflowCalls++
	cp := *w // snapshot: later archiving must not retroactively change what this call already returned
	if f.archiveAfterFindWorkflowCall != 0 && f.findWorkflowCalls == f.archiveAfterFindWorkflowCall {
		w.Status = workflowdom.StatusArchived
	}
	return &cp, nil
}

func (f *fakeGraphStore) FindNodeByID(_ context.Context, id uuid.UUID) (*workflowdom.Node, error) {
	n, ok := f.nodes[id]
	if !ok {
		return nil, workflowdom.ErrNodeNotFound
	}
	return n, nil
}

func (f *fakeGraphStore) ListActiveNodesByTaskID(_ context.Context, taskID uuid.UUID) ([]*workflowdom.Node, error) {
	var out []*workflowdom.Node
	for _, n := range f.nodes {
		if n.TaskID != taskID {
			continue
		}
		if w, ok := f.workflows[n.WorkflowID]; ok && w.Status == workflowdom.StatusActive {
			out = append(out, n)
		}
	}
	return out, nil
}

func (f *fakeGraphStore) ListStatusRulesByWorkflow(_ context.Context, workflowID uuid.UUID) ([]*workflowdom.StatusRule, error) {
	f.rulesCalls++
	rules := f.rules[workflowID]
	// Snapshot each rule, same as FindWorkflowByID above: a later mutation to
	// the underlying rule (simulating a concurrent SetStatusRule edit) must
	// not retroactively change what an earlier call already returned.
	out := make([]*workflowdom.StatusRule, len(rules))
	for i, r := range rules {
		cp := *r
		out[i] = &cp
	}
	if f.afterListStatusRulesCall != nil {
		f.afterListStatusRulesCall(f.rulesCalls)
	}
	return out, nil
}

func (f *fakeGraphStore) ListStatusTransitionsByWorkflow(_ context.Context, workflowID uuid.UUID) ([]*workflowdom.StatusTransition, error) {
	f.transitionsCalls++
	return f.transitions[workflowID], nil
}

func (f *fakeGraphStore) ListEdgesByWorkflow(_ context.Context, workflowID uuid.UUID) ([]*workflowdom.Edge, error) {
	var out []*workflowdom.Edge
	for _, e := range f.edges {
		if e.WorkflowID == workflowID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeGraphStore) ListIncomingEdges(_ context.Context, targetNodeID uuid.UUID) ([]*workflowdom.Edge, error) {
	var out []*workflowdom.Edge
	for _, e := range f.edges {
		if e.TargetNodeID == targetNodeID {
			out = append(out, e)
		}
	}
	return out, nil
}

// fakeTaskStore implements both workflowTaskReader and workflowTaskUpdater.
type fakeTaskStore struct {
	tasks       map[uuid.UUID]*taskdom.Task
	statuses    map[uuid.UUID]*taskdom.TaskStatus
	updateCalls int
}

func newFakeTaskStore() *fakeTaskStore {
	return &fakeTaskStore{
		tasks:    make(map[uuid.UUID]*taskdom.Task),
		statuses: make(map[uuid.UUID]*taskdom.TaskStatus),
	}
}

func (f *fakeTaskStore) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTaskStore) FindTaskStatusByID(_ context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	s, ok := f.statuses[id]
	if !ok {
		return nil, taskdom.ErrStatusNotFound
	}
	return s, nil
}

func (f *fakeTaskStore) UpdateTask(_ context.Context, _, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	f.updateCalls++
	t, ok := f.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	if in.AssigneeIDs != nil {
		t.AssigneeIDs = *in.AssigneeIDs
	}
	cp := *t
	return &cp, nil
}

type fakeActivityRecorder struct{ calls int }

func (f *fakeActivityRecorder) RecordActivity(_ context.Context, _ taskdom.RecordActivityInput) error {
	f.calls++
	return nil
}

// ---------------------------------------------------------------------------
// Test fixture
// ---------------------------------------------------------------------------

type engineFixture struct {
	graph    *fakeGraphStore
	tasks    *fakeTaskStore
	activity *fakeActivityRecorder
	consumer *WorkflowConsumer

	projectID uuid.UUID
	doneStatus,
	readyStatus *taskdom.TaskStatus
}

func newEngineFixture() *engineFixture {
	graph := newFakeGraphStore()
	tasks := newFakeTaskStore()
	activity := &fakeActivityRecorder{}
	projectID := uuid.New()

	doneStatus := &taskdom.TaskStatus{ID: uuid.New(), ProjectID: projectID, Name: "Done", Category: taskdom.StatusCategoryDone}
	readyStatus := &taskdom.TaskStatus{ID: uuid.New(), ProjectID: projectID, Name: "Ready", Category: taskdom.StatusCategoryTodo}
	tasks.statuses[doneStatus.ID] = doneStatus
	tasks.statuses[readyStatus.ID] = readyStatus

	return &engineFixture{
		graph:       graph,
		tasks:       tasks,
		activity:    activity,
		projectID:   projectID,
		doneStatus:  doneStatus,
		readyStatus: readyStatus,
		consumer: &WorkflowConsumer{
			workflowRepo: graph,
			taskRepo:     tasks,
			taskSvc:      tasks,
			activityRec:  activity,
			log:          discardLogger(),
		},
	}
}

func (f *engineFixture) addWorkflow() *workflowdom.Workflow {
	w := &workflowdom.Workflow{ID: uuid.New(), ProjectID: f.projectID, Status: workflowdom.StatusActive, Name: "wf"}
	f.graph.workflows[w.ID] = w
	return w
}

func (f *engineFixture) addNode(w *workflowdom.Workflow, statusID *uuid.UUID) (*workflowdom.Node, *taskdom.Task) {
	task := &taskdom.Task{ID: uuid.New(), ProjectID: f.projectID, StatusID: statusID}
	f.tasks.tasks[task.ID] = task
	node := &workflowdom.Node{ID: uuid.New(), WorkflowID: w.ID, TaskID: task.ID}
	f.graph.nodes[node.ID] = node
	return node, task
}

func (f *engineFixture) addRule(w *workflowdom.Workflow, statusID, memberID uuid.UUID) {
	f.graph.rules[w.ID] = append(f.graph.rules[w.ID], &workflowdom.StatusRule{
		ID: uuid.New(), WorkflowID: w.ID, StatusID: statusID, AssigneeMemberID: memberID,
	})
}

// addTransition sets, for the workflow as a whole, what status comes next
// after statusID. nextStatusID nil marks statusID as the workflow's
// terminal/done status.
func (f *engineFixture) addTransition(w *workflowdom.Workflow, statusID uuid.UUID, nextStatusID *uuid.UUID) {
	f.graph.transitions[w.ID] = append(f.graph.transitions[w.ID], &workflowdom.StatusTransition{
		ID: uuid.New(), WorkflowID: w.ID, StatusID: statusID, NextStatusID: nextStatusID,
	})
}

func (f *engineFixture) addEdge(w *workflowdom.Workflow, source, target *workflowdom.Node) {
	f.graph.edges = append(f.graph.edges, &workflowdom.Edge{
		ID: uuid.New(), WorkflowID: w.ID, SourceNodeID: source.ID, TargetNodeID: target.ID,
	})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestApplyNode_EventOne_DirectStatusRule(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	member := uuid.New()

	_, task := f.addNode(w, &f.doneStatus.ID)
	f.addRule(w, f.doneStatus.ID, member)

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, task.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	got := f.tasks.tasks[task.ID]
	if !isAssignedOnlyTo(got, member) {
		t.Fatalf("expected task assigned to %v, got %+v", member, got.AssigneeIDs)
	}
	if f.tasks.updateCalls != 1 {
		t.Fatalf("expected exactly 1 UpdateTask call, got %d", f.tasks.updateCalls)
	}
}

func TestApplyNode_EventOne_IdempotentWhenAlreadyAssigned(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	member := uuid.New()

	_, task := f.addNode(w, &f.doneStatus.ID)
	f.addRule(w, f.doneStatus.ID, member)
	task.AssigneeIDs = []uuid.UUID{member} // already assigned before the event fires

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, task.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}
	if f.tasks.updateCalls != 0 {
		t.Fatalf("expected no UpdateTask call when already assigned, got %d", f.tasks.updateCalls)
	}
}

func TestChain_SinglePredecessor_AssignsDownstreamOnDone(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	downstreamMember := uuid.New()

	nodeA, taskA := f.addNode(w, &f.doneStatus.ID)
	nodeB, taskB := f.addNode(w, &f.readyStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil) // doneStatus is this workflow's terminal/done status
	f.addRule(w, f.readyStatus.ID, downstreamMember)
	f.addEdge(w, nodeA, nodeB)

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskA.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	gotB := f.tasks.tasks[taskB.ID]
	if !isAssignedOnlyTo(gotB, downstreamMember) {
		t.Fatalf("expected downstream task assigned to %v, got %+v", downstreamMember, gotB.AssigneeIDs)
	}
}

func TestDiamond_ANDJoin_WaitsForAllPredecessors(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	downstreamMember := uuid.New()

	nodeA, taskA := f.addNode(w, &f.readyStatus.ID)
	nodeB, taskB := f.addNode(w, &f.readyStatus.ID)
	nodeC, taskC := f.addNode(w, &f.readyStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil) // doneStatus is this workflow's terminal/done status
	f.addRule(w, f.readyStatus.ID, downstreamMember)
	f.addEdge(w, nodeA, nodeC)
	f.addEdge(w, nodeB, nodeC)

	// A finishes, but B has not — C must NOT be assigned yet.
	taskA.StatusID = &f.doneStatus.ID
	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskA.ID); err != nil {
		t.Fatalf("processTaskStatusChange(A): %v", err)
	}
	if got := f.tasks.tasks[taskC.ID]; len(got.AssigneeIDs) != 0 {
		t.Fatalf("expected C to remain unassigned while B is not done, got %+v", got.AssigneeIDs)
	}

	// B finishes too — NOW C should be assigned.
	taskB.StatusID = &f.doneStatus.ID
	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskB.ID); err != nil {
		t.Fatalf("processTaskStatusChange(B): %v", err)
	}
	gotC := f.tasks.tasks[taskC.ID]
	if !isAssignedOnlyTo(gotC, downstreamMember) {
		t.Fatalf("expected C assigned to %v once both predecessors are done, got %+v", downstreamMember, gotC.AssigneeIDs)
	}
}

func TestIsNodeDone_DerivesFromWorkflowTransitions(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	f.addTransition(w, f.readyStatus.ID, &f.doneStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil) // terminal

	node, task := f.addNode(w, &f.readyStatus.ID)
	task.StatusID = &f.doneStatus.ID

	done, err := f.consumer.isNodeDone(ctx, node, task, newEvalCache())
	if err != nil {
		t.Fatalf("isNodeDone: %v", err)
	}
	if !done {
		t.Fatalf("expected node to be considered done once its task reaches the workflow's derived done status")
	}
}

func TestIsNodeDone_FalseWhenChainHasNoUniqueTerminal(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	// No transitions configured at all — nothing to derive a done status from.

	node, task := f.addNode(w, &f.readyStatus.ID)
	task.StatusID = &f.doneStatus.ID

	done, err := f.consumer.isNodeDone(ctx, node, task, newEvalCache())
	if err != nil {
		t.Fatalf("isNodeDone: %v", err)
	}
	if done {
		t.Fatalf("expected node to not be considered done when no unique terminal status is configured")
	}
}

// TestProcessTaskStatusChange_SecondNodeSeesFreshAssigneeFromFirst guards
// against a task that belongs to nodes in two different active workflows
// getting reassigned twice in the same event: the second node's idempotency
// check must see the first node's just-applied assignee rather than a
// stale in-memory copy of the task.
func TestProcessTaskStatusChange_SecondNodeSeesFreshAssigneeFromFirst(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w1 := f.addWorkflow()
	w2 := f.addWorkflow()
	member := uuid.New()

	task := &taskdom.Task{ID: uuid.New(), ProjectID: f.projectID, StatusID: &f.doneStatus.ID}
	f.tasks.tasks[task.ID] = task
	node1 := &workflowdom.Node{ID: uuid.New(), WorkflowID: w1.ID, TaskID: task.ID}
	node2 := &workflowdom.Node{ID: uuid.New(), WorkflowID: w2.ID, TaskID: task.ID}
	f.graph.nodes[node1.ID] = node1
	f.graph.nodes[node2.ID] = node2
	f.addRule(w1, f.doneStatus.ID, member)
	f.addRule(w2, f.doneStatus.ID, member)

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, task.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	if f.tasks.updateCalls != 1 {
		t.Fatalf("expected exactly 1 UpdateTask call — the second node's idempotency check should see the first node's assignment instead of a stale copy — got %d", f.tasks.updateCalls)
	}
}

// TestApplyStatusRule_SkipsWhenWorkflowNoLongerActive guards against the
// engine completing a reassignment against a workflow that was archived (or
// reverted to draft) after ListActiveNodesByTaskID already returned this
// node but before the mutating UpdateTask call runs.
func TestApplyStatusRule_SkipsWhenWorkflowNoLongerActive(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	member := uuid.New()

	node, task := f.addNode(w, &f.doneStatus.ID)
	f.addRule(w, f.doneStatus.ID, member)

	// Simulate archival landing in the window between the node being
	// selected for evaluation and applyStatusRule actually running.
	w.Status = workflowdom.StatusArchived

	if err := f.consumer.applyStatusRule(ctx, f.projectID, node, task, "status_rule", newEvalCache()); err != nil {
		t.Fatalf("applyStatusRule: %v", err)
	}
	if f.tasks.updateCalls != 0 {
		t.Fatalf("expected no UpdateTask call once the workflow is no longer active, got %d", f.tasks.updateCalls)
	}
}

// TestApplyStatusRule_ArchivedMidFanOut_StopsLaterNodes guards against the
// workflow's active/archived status being memoized for the whole event: one
// processTaskStatusChange call can fan out across several nodes/edges (an
// AND-join cascade), and each node's applyStatusRule call must see the
// workflow's current status, not whatever the first node in the fan-out saw.
func TestApplyStatusRule_ArchivedMidFanOut_StopsLaterNodes(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	memberA := uuid.New()
	downstreamMember := uuid.New()

	nodeA, taskA := f.addNode(w, &f.doneStatus.ID)
	nodeB, taskB := f.addNode(w, &f.readyStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil)         // doneStatus is this workflow's terminal/done status
	f.addRule(w, f.doneStatus.ID, memberA)           // nodeA's own reassignment (event 1)
	f.addRule(w, f.readyStatus.ID, downstreamMember) // nodeB's reassignment once nodeA is done (event 2)
	f.addEdge(w, nodeA, nodeB)

	// Simulate the workflow being archived (e.g. via the HTTP handler)
	// right after nodeA's own gate check reads "active" but before nodeB's
	// predecessor-done gate check runs later in this same event's fan-out.
	f.graph.archiveAfterFindWorkflowCall = 1

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskA.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	gotA := f.tasks.tasks[taskA.ID]
	if !isAssignedOnlyTo(gotA, memberA) {
		t.Fatalf("expected nodeA's own reassignment to complete before the archive landed, got %+v", gotA.AssigneeIDs)
	}
	gotB := f.tasks.tasks[taskB.ID]
	if len(gotB.AssigneeIDs) != 0 {
		t.Fatalf("expected nodeB to NOT be reassigned once the workflow was archived mid-fan-out, got %v", gotB.AssigneeIDs)
	}
	if f.tasks.updateCalls != 1 {
		t.Fatalf("expected exactly 1 UpdateTask call (nodeA only, before the archive), got %d", f.tasks.updateCalls)
	}
}

// TestApplyStatusRule_ReflectsRuleEditedMidFanOut guards against a status
// rule's assignee being memoized for the whole event: rules can now be
// edited while a workflow stays active (unlike before, when editing required
// reverting to draft first, which the workflow-status freshness check above
// would have caught). One processTaskStatusChange call can fan out across
// several nodes in the same workflow (an A->B chain), and nodeB's rule
// lookup must see a concurrent edit landing between nodeA's and nodeB's
// evaluation rather than reusing nodeA's now-stale snapshot.
func TestApplyStatusRule_ReflectsRuleEditedMidFanOut(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	memberA := uuid.New()
	oldMemberB := uuid.New()
	newMemberB := uuid.New()

	nodeA, taskA := f.addNode(w, &f.doneStatus.ID)
	nodeB, taskB := f.addNode(w, &f.readyStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil) // doneStatus is this workflow's terminal/done status
	f.addRule(w, f.doneStatus.ID, memberA)   // nodeA's own reassignment (event 1)
	ruleB := &workflowdom.StatusRule{ID: uuid.New(), WorkflowID: w.ID, StatusID: f.readyStatus.ID, AssigneeMemberID: oldMemberB}
	f.graph.rules[w.ID] = append(f.graph.rules[w.ID], ruleB)
	f.addEdge(w, nodeA, nodeB)

	// Simulate a concurrent SetStatusRule call (e.g. via the HTTP handler)
	// changing nodeB's rule right after nodeA's rules lookup (event 1) but
	// before nodeB's (event 2, fired once nodeA is done) — both reads happen
	// inside the same processTaskStatusChange fan-out.
	f.graph.afterListStatusRulesCall = func(call int) {
		if call == 1 {
			ruleB.AssigneeMemberID = newMemberB
		}
	}

	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskA.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	gotB := f.tasks.tasks[taskB.ID]
	if !isAssignedOnlyTo(gotB, newMemberB) {
		t.Fatalf("expected nodeB to be assigned the rule's up-to-date member %v (edited concurrently mid-fan-out), got %+v", newMemberB, gotB.AssigneeIDs)
	}
	if f.graph.rulesCalls != 2 {
		t.Fatalf("expected rules to be re-read fresh for each node in the fan-out (not cached), got %d calls", f.graph.rulesCalls)
	}
}

// TestDiamond_ANDJoin_CachesTransitionsAcrossPredecessors guards against the
// N+1 query pattern where checking an AND-join's predecessors re-fetches the
// same workflow's status transitions once per predecessor (plus once more
// for the node itself and once more for the next-status-name lookup).
func TestDiamond_ANDJoin_CachesTransitionsAcrossPredecessors(t *testing.T) {
	f := newEngineFixture()
	ctx := context.Background()
	w := f.addWorkflow()
	downstreamMember := uuid.New()

	nodeA, taskA := f.addNode(w, &f.doneStatus.ID)
	nodeB, _ := f.addNode(w, &f.doneStatus.ID)
	nodeC, _ := f.addNode(w, &f.readyStatus.ID)
	f.addTransition(w, f.doneStatus.ID, nil) // doneStatus is this workflow's terminal/done status
	f.addRule(w, f.readyStatus.ID, downstreamMember)
	f.addEdge(w, nodeA, nodeC)
	f.addEdge(w, nodeB, nodeC)

	// Both predecessors already done: a single status-change event on A
	// evaluates isNodeDone for A (directly) and for A and B again (inside
	// tryFireEdge's AND-join loop) — all against the same workflow — plus
	// applyStatusRule's next-status lookup for C. All of those should share
	// one real repository call.
	if err := f.consumer.processTaskStatusChange(ctx, f.projectID, taskA.ID); err != nil {
		t.Fatalf("processTaskStatusChange: %v", err)
	}

	if f.graph.transitionsCalls != 1 {
		t.Fatalf("expected the per-event cache to collapse repeated ListStatusTransitionsByWorkflow calls into 1, got %d", f.graph.transitionsCalls)
	}
}
