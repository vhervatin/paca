import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockPut, mockDelete } = vi.hoisted(
	() => ({
		mockGet: vi.fn(),
		mockPost: vi.fn(),
		mockPatch: vi.fn(),
		mockPut: vi.fn(),
		mockDelete: vi.fn(),
	}),
);

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			put: mockPut,
			delete: mockDelete,
		},
	},
}));

import {
	createServiceAccount,
	createServiceAccountKey,
	deleteServiceAccount,
	getServiceAccount,
	listServiceAccountKeys,
	listServiceAccountRoles,
	listServiceAccounts,
	replaceServiceAccountRoles,
	revokeServiceAccountKey,
	type ServiceAccount,
	serviceAccountKeysQueryOptions,
	serviceAccountsQueryOptions,
	updateServiceAccount,
} from "./service-account-api";

const envelope = <T>(data: T) => ({ data: { data, error_code: null } });

const account: ServiceAccount = {
	id: "sa-1",
	name: "CI bot",
	description: "deploys",
	roles: [],
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

describe("service-account-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("lists service accounts from /admin/service-accounts", async () => {
		mockGet.mockResolvedValue(envelope([account]));
		await expect(listServiceAccounts()).resolves.toEqual([account]);
		expect(mockGet).toHaveBeenCalledWith("/admin/service-accounts");
	});

	it("reads one account", async () => {
		mockGet.mockResolvedValue(envelope(account));
		await expect(getServiceAccount("sa-1")).resolves.toEqual(account);
		expect(mockGet).toHaveBeenCalledWith("/admin/service-accounts/sa-1");
	});

	it("creates and updates an account", async () => {
		mockPost.mockResolvedValue(envelope(account));
		await expect(
			createServiceAccount({ name: "CI bot", description: "deploys" }),
		).resolves.toEqual(account);
		expect(mockPost).toHaveBeenCalledWith("/admin/service-accounts", {
			name: "CI bot",
			description: "deploys",
		});

		mockPatch.mockResolvedValue(envelope({ ...account, name: "Release bot" }));
		await expect(
			updateServiceAccount("sa-1", { name: "Release bot" }),
		).resolves.toMatchObject({ name: "Release bot" });
		expect(mockPatch).toHaveBeenCalledWith("/admin/service-accounts/sa-1", {
			name: "Release bot",
		});
	});

	it("deletes an account", async () => {
		mockDelete.mockResolvedValue({});
		await deleteServiceAccount("sa-1");
		expect(mockDelete).toHaveBeenCalledWith("/admin/service-accounts/sa-1");
	});

	it("manages an account's API keys under its own path", async () => {
		mockGet.mockResolvedValue(envelope([]));
		await listServiceAccountKeys("sa-1");
		expect(mockGet).toHaveBeenCalledWith(
			"/admin/service-accounts/sa-1/api-keys",
		);

		mockPost.mockResolvedValue(
			envelope({ id: "k1", name: "ci", key: "paca_x" }),
		);
		await expect(
			createServiceAccountKey("sa-1", { name: "ci", expires_at: null }),
		).resolves.toMatchObject({ key: "paca_x" });
		expect(mockPost).toHaveBeenCalledWith(
			"/admin/service-accounts/sa-1/api-keys",
			{ name: "ci", expires_at: null },
		);

		mockDelete.mockResolvedValue({});
		await revokeServiceAccountKey("sa-1", "k1");
		expect(mockDelete).toHaveBeenCalledWith(
			"/admin/service-accounts/sa-1/api-keys/k1",
		);
	});

	it("reads and replaces an account's roles", async () => {
		mockGet.mockResolvedValue(envelope([]));
		await listServiceAccountRoles("sa-1");
		expect(mockGet).toHaveBeenCalledWith("/admin/service-accounts/sa-1/roles");

		mockPut.mockResolvedValue(envelope([]));
		await replaceServiceAccountRoles("sa-1", ["r1", "r2"]);
		expect(mockPut).toHaveBeenCalledWith("/admin/service-accounts/sa-1/roles", {
			role_ids: ["r1", "r2"],
		});
	});

	it("keys the caches per account so one account's keys never show for another", () => {
		expect(serviceAccountsQueryOptions.queryKey).toEqual([
			"admin",
			"service-accounts",
		]);
		expect(serviceAccountKeysQueryOptions("a").queryKey).not.toEqual(
			serviceAccountKeysQueryOptions("b").queryKey,
		);
	});
});
