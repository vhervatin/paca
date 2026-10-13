import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useActorAvatar } from "./use-actor-avatar";

vi.mock("@/lib/project-api", () => ({
	projectMembersQueryOptions: () => ({
		queryKey: ["members-test"],
		queryFn: async () => [],
		initialData: [
			{
				id: "m1",
				user_id: "u1",
				full_name: "Bot",
				username: "bot",
				member_type: "agent",
				agent_type: "llm",
				agent_llm_provider: "openai",
			},
		],
	}),
}));

function wrapper({ children }: { children: ReactNode }) {
	const client = new QueryClient();
	return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useActorAvatar", () => {
	it("resolves a member by member id or account id", () => {
		const { result } = renderHook(() => useActorAvatar("p1"), { wrapper });
		expect(result.current({ id: "m1" }).type).toBe("agent");
		expect(result.current({ id: "u1" }).type).toBe("agent");
	});

	it("falls back to a human identity for a former member", () => {
		const { result } = renderHook(() => useActorAvatar("p1"), { wrapper });
		expect(
			result.current({ id: "gone", name: "Ex", avatarUrl: "https://x/a.png" }),
		).toEqual({ name: "Ex", type: "human", avatarUrl: "https://x/a.png" });
	});
});
