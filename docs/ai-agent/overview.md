# AI Agent Feature — Overview

Paca AI Agents are first-class project members powered by the [OpenHands Software Agent SDK](https://docs.openhands.dev/sdk). Each agent runs in an isolated Docker container and can be triggered by task assignment, comment @mentions, or direct chat. Agents participate in the project exactly like human members — they appear in member lists, can be assigned tasks, and exchange messages in comments and chats.

## Table of Contents

- [Concepts](#concepts)
- [Architecture](#architecture)
- [Trigger Model](#trigger-model)
- [Conversation Lifecycle](#conversation-lifecycle)
- [Repository Access & PR Creation](#repository-access--pr-creation)
- [Default Agent Types](#default-agent-types)
- [Customization](#customization)
- [Related Documents](#related-documents)

---

## Concepts

| Term | Meaning |
|---|---|
| **Agent** | A project-scoped AI entity with a role, LLM config, skills, MCP servers, and a system prompt. |
| **Agent Member** | A `project_members` row with `member_type = 'agent'` and a reference to the `agents` table. Agents are treated identically to human members in all product surfaces. |
| **Agent Type** | A template that pre-fills LLM, skills, and system prompt. Built-in types: PO Assistant, Business Analyst, Developer, Manual Tester. Users can create custom types. |
| **Agent Conversation** | A single OpenHands SDK `Conversation` session, spawned in a dedicated Docker container for each trigger event. |
| **Conversation Event** | An atomic action/observation within a conversation (LLM message, bash command, file edit, etc.). Persisted to the database for history and real-time monitoring. |
| **Trigger** | An event that creates an agent conversation: task assignment, comment @mention, or direct chat message. |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  apps/web                                                                   │
│  • Agent management UI (project settings)                                   │
│  • Real-time conversation monitoring (stop / continue / history)            │
│  • @mention autocomplete for agents in comments                             │
│  • Direct chat panel with agents                                            │
└─────────────────┬───────────────────────────────────────────────────────────┘
                  │ HTTP / Socket.IO
                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  services/api  (Go + Gin)                                                   │
│  • Agent CRUD (domain: agent)                                               │
│  • Publishing agent-trigger events → Valkey Stream "paca:agent:triggers"   │
│  • REST endpoints for conversation history & control                        │
│  • Writing conversation summaries / replies back to tasks/comments          │
└──────────┬───────────────────────────────┬──────────────────────────────────┘
           │  Valkey Stream (triggers)      │  Valkey Stream (events back)
           ▼                                ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  services/ai-agent  (Python + FastAPI + OpenHands SDK)                      │
│  • Stream consumer: reads "paca:agent:triggers"                             │
│  • Spawns one DockerWorkspace per conversation                              │
│  • Runs OpenHands Conversation inside the container                         │
│  • Publishes conversation events → Valkey Stream "paca:agent:events"        │
│  • REST endpoints: pause, resume, stop, history                             │
└──────────────────────────────────────────────────────────────────────────────┘
                  │
                  │  Docker socket (spawn / manage containers)
                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Agent Docker Containers  (ghcr.io/paca-ai/paca-agent-server:latest)        │
│  • One container per active conversation                                    │
│  • Completely isolated from other containers                                │
│  • Workspace cloned from repo plugin (credentials injected as secrets)     │
│  • Destroyed when conversation finishes / is stopped                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Service Responsibilities

| Service | Responsibility |
|---|---|
| `services/api` | Owns agent configuration, triggers agent invocations, stores conversation summaries and replies, exposes control APIs. |
| `services/ai-agent` | Executes agent conversations via OpenHands SDK, manages Docker container lifecycle, streams events back. |
| `services/realtime` | Delivers real-time conversation events to the web client via Socket.IO (same existing Valkey→Socket.IO fan-out). |
| Docker host | Provides container isolation. Agent containers cannot reach other Paca service containers on the internal network by default. |

---

## Trigger Model

### 1. Task Assignment

A task can have multiple assignees (stored in the `task_assignees` join table). When one of a task's assignees points to a `project_members` row with `member_type = 'agent'`, the API publishes an `agent.task.assigned` event to the Valkey Stream — one event per newly-added agent assignee — containing:

```json
{
  "trigger_type": "task_assigned",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "task_id": "<uuid>",
  "task_title": "...",
  "task_description": "...",
  "actor_member_id": "<uuid>"
}
```

The agent service picks this up, spins up a conversation, and instructs the agent to work on the task. When complete, the agent posts a summary comment on the task and optionally creates a PR.

### 2. Comment @mention

When a comment body contains `@<agent-handle>`, the API publishes an `agent.comment.mention` event:

```json
{
  "trigger_type": "comment_mention",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "task_id": "<uuid>",
  "comment_id": "<uuid>",
  "comment_body": "...",
  "actor_member_id": "<uuid>"
}
```

The agent responds directly in the comment thread.

### 3. Direct Chat

A dedicated chat API allows users to send messages to an agent member. Internally this publishes an `agent.chat.message` event and opens (or resumes) a persistent conversation per agent per user.

```json
{
  "trigger_type": "chat_message",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "chat_session_id": "<uuid>",
  "message": "...",
  "actor_member_id": "<uuid>"
}
```

---

## Conversation Lifecycle

```
Trigger event published
        │
        ▼
ai-agent service dequeues event
        │
        ▼
Resolve agent config (LLM, skills, MCP servers, system prompt)
        │
        ▼
Clone repository (if coding task) via repository plugin adapter
  - fetch clone URL + temporary token from plugin
  - inject credentials as OpenHands SecretSource (never logged)
        │
        ▼
Spawn DockerWorkspace (OpenHands agent-server image)
        │
        ▼
Create OpenHands Conversation with:
  - LLM from agent config
  - Skills from agent config
  - MCP servers from agent config
  - System prompt from agent config
  - Conversation ID stored in DB
  - Persistence dir mounted into container
  - Event callback → publish to Valkey "paca:agent:events"
        │
        ├─── User sends "pause" → conversation.pause()
        ├─── User sends "resume" → conversation.run()
        ├─── User sends "stop" → conversation.close(), container destroyed
        │
        ▼
Conversation finishes (agent sends finish action)
        │
        ▼
Persist summary + outputs
  - Post reply comment / chat message via API
  - Create PR if coding task (via repo plugin)
        │
        ▼
Container destroyed, conversation state archived
```

---

## Repository Access & PR Creation

Agents must be able to read and write code without ever seeing VCS credentials directly.

### Clone Flow

1. When the trigger involves a coding task, `services/ai-agent` calls the **repository plugin adapter** endpoint (e.g., the GitHub plugin) with the project context.
2. The plugin returns a **short-lived scoped token** (e.g., a GitHub installation token with read/write on the repository, valid for 10 minutes) and the HTTPS clone URL.
3. The token is injected into the OpenHands `Conversation` via `conversation.update_secrets()` as a `SecretSource` that fetches a fresh token on demand — the token value never appears in any log or agent output.
4. The agent's first tool call clones the repository: `git clone https://x-access-token:$GIT_TOKEN@github.com/org/repo.git`.
5. When the conversation ends, the workspace is destroyed and the token expires automatically.

### PR Creation Flow

1. The agent completes coding work and signals readiness in its finish message.
2. `services/ai-agent` calls the repository plugin adapter's **create PR endpoint** with the branch name and description generated by the agent.
3. The plugin creates the PR and returns the PR URL.
4. The agent service posts the PR URL as a comment on the Paca task.

This design means:
- Agents never store credentials.
- Credentials are not readable from container logs (masked by `SecretSource`).
- Plugin plugins remain the single source of truth for VCS auth.

---

## Default Agent Types

| Type | Role | Default LLM | Pre-loaded Skills |
|---|---|---|---|
| **PO Assistant** | Product Owner — backlog grooming, acceptance criteria, prioritization | `anthropic/claude-sonnet-4-6` | `po-assistant` skill with Agile PO guidelines |
| **Business Analyst** | Requirements analysis, user story writing, gap analysis | `anthropic/claude-sonnet-4-6` | `ba-assistant` skill |
| **Developer** | Coding, code review, PR creation, bug fixing | `anthropic/claude-sonnet-4-6` | `developer` skill + `github`/`gitlab` skills |
| **Manual Tester** | Test case design, exploratory testing docs, defect analysis | `anthropic/claude-sonnet-4-6` | `manual-tester` skill |

Users can create custom agent types with any combination of LLM provider, skills, MCP servers, and system prompt.

---

## Customization

Every agent exposes four customization axes:

| Axis | Description |
|---|---|
| **LLM Provider** | Any LiteLLM-supported provider: Anthropic, OpenAI, Azure, AWS Bedrock, Gemini, Groq, OpenRouter, local LLMs, etc. |
| **System Prompt** | Free-form Jinja2 template or plain text, pre-filled from the agent type. |
| **Skills** | AgentSkills-standard `SKILL.md` directories or inline text skills. Stored in the DB, mounted into the container at runtime. |
| **MCP Servers** | JSON MCP config following the standard `mcpServers` format. Evaluated inside the container at conversation start. |

---

## Related Documents

- [database-schema.md](database-schema.md) — Agent tables and modifications to `project_members`
- [api-design.md](api-design.md) — REST endpoints for agent management
- [ai-agent-service.md](ai-agent-service.md) — `services/ai-agent` implementation details
- [repository-plugin-adapter.md](repository-plugin-adapter.md) — How agents access VCS credentials
- [realtime-events.md](realtime-events.md) — Socket.IO events emitted during conversations
