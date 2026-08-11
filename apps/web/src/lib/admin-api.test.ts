import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockDelete } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPost: vi.fn(),
	mockPatch: vi.fn(),
	mockDelete: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			delete: mockDelete,
		},
	},
}));

import {
	ADMIN_USERS_PAGE_SIZE,
	createGlobalRole,
	createUser,
	deleteGlobalRole,
	deleteUser,
	type GlobalRole,
	getGlobalRoles,
	getMyGlobalPermissions,
	getUsers,
	globalRolesQueryOptions,
	myPermissionsQueryOptions,
	resetUserPassword,
	type User,
	updateGlobalRole,
	updateUser,
	usersInfiniteQueryOptions,
	usersQueryOptions,
} from "./admin-api";

// The backend caps page_size for /admin/users at 100 and rejects anything
// larger with HTTP 400. A request over the cap makes the "add team member"
// picker come up empty ("no users available"). Guard both constants and the
// infinite-query page size so that regression can't come back.
const ADMIN_USERS_MAX_PAGE_SIZE = 100;

describe("admin-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("unwraps global roles from response", async () => {
		const roles: GlobalRole[] = [
			{
				id: "r1",
				name: "Admin",
				permissions: { "users.manage": true },
				created_at: "2026-03-29T00:00:00.000Z",
				updated_at: "2026-03-29T00:00:00.000Z",
			},
		];
		mockGet.mockResolvedValue({
			data: { data: roles, error_code: null, message: "ok" },
		});

		await expect(getGlobalRoles()).resolves.toEqual(roles);
		expect(mockGet).toHaveBeenCalledWith("/admin/global-roles");
	});

	it("posts payload to create role and unwraps response", async () => {
		const payload = {
			name: "Editor",
			permissions: { "projects.read": true },
		};
		const created: GlobalRole = {
			id: "r2",
			name: "Editor",
			permissions: payload.permissions,
			created_at: "2026-03-29T00:00:00.000Z",
			updated_at: "2026-03-29T00:00:00.000Z",
		};
		mockPost.mockResolvedValue({
			data: { data: created, error_code: null, message: "ok" },
		});

		await expect(createGlobalRole(payload)).resolves.toEqual(created);
		expect(mockPost).toHaveBeenCalledWith("/admin/global-roles", payload);
	});

	it("patches role by id and unwraps response", async () => {
		const payload = {
			name: "Support",
			permissions: { "tickets.read": true },
		};
		const updated: GlobalRole = {
			id: "r3",
			name: "Support",
			permissions: payload.permissions,
			created_at: "2026-03-29T00:00:00.000Z",
			updated_at: "2026-03-29T00:01:00.000Z",
		};
		mockPatch.mockResolvedValue({
			data: { data: updated, error_code: null, message: "ok" },
		});

		await expect(updateGlobalRole("r3", payload)).resolves.toEqual(updated);
		expect(mockPatch).toHaveBeenCalledWith("/admin/global-roles/r3", payload);
	});

	it("deletes role by id", async () => {
		mockDelete.mockResolvedValue({});

		await expect(deleteGlobalRole("r4")).resolves.toBeUndefined();
		expect(mockDelete).toHaveBeenCalledWith("/admin/global-roles/r4");
	});

	it("unwraps global permissions list from response", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: { permissions: ["users.read", "projects.*"] },
				error_code: null,
				message: "ok",
			},
		});

		await expect(getMyGlobalPermissions()).resolves.toEqual([
			"users.read",
			"projects.*",
		]);
		expect(mockGet).toHaveBeenCalledWith("/users/me/global-permissions");
	});

	it("exposes query option contracts", () => {
		expect(globalRolesQueryOptions.queryKey).toEqual(["admin", "global-roles"]);
		expect(typeof globalRolesQueryOptions.queryFn).toBe("function");

		expect(myPermissionsQueryOptions.queryKey).toEqual([
			"auth",
			"me",
			"permissions",
		]);
		expect(typeof myPermissionsQueryOptions.queryFn).toBe("function");
		expect(myPermissionsQueryOptions.staleTime).toBe(5 * 60 * 1000);
		expect(myPermissionsQueryOptions.retry).toBe(false);
	});

	// Users API
	// -----------------------------------------------------------------------

	const mockUser: User = {
		id: "u1",
		username: "alice",
		full_name: "Alice Smith",
		role: "Admin",
		must_change_password: false,
		created_at: "2026-01-01T00:00:00.000Z",
	};

	it("fetches paged users", async () => {
		const response = {
			items: [mockUser],
			total: 1,
			page: 1,
			page_size: 20,
		};
		mockGet.mockResolvedValue({
			data: { data: response, error_code: null, message: "ok" },
		});

		await expect(getUsers(1, 20)).resolves.toEqual(response);
		expect(mockGet).toHaveBeenCalledWith("/admin/users", {
			params: { page: 1, page_size: 20 },
		});
	});

	it("uses default page and page_size for getUsers", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: { items: [], total: 0, page: 1, page_size: 20 },
				error_code: null,
				message: "ok",
			},
		});

		await getUsers();
		expect(mockGet).toHaveBeenCalledWith("/admin/users", {
			params: { page: 1, page_size: 20 },
		});
	});

	it("posts payload to create user and unwraps response", async () => {
		mockPost.mockResolvedValue({
			data: { data: mockUser, error_code: null, message: "ok" },
		});

		const payload = {
			username: "alice",
			password: "P@ssw0rd!",
			full_name: "Alice Smith",
			email: "alice@example.com",
			role: "Admin",
		};

		await expect(createUser(payload)).resolves.toEqual(mockUser);
		expect(mockPost).toHaveBeenCalledWith("/admin/users", payload);
	});

	it("patches user by id and unwraps response", async () => {
		const updated = { ...mockUser, full_name: "Alice J. Smith" };
		mockPatch.mockResolvedValue({
			data: { data: updated, error_code: null, message: "ok" },
		});

		await expect(
			updateUser("u1", { full_name: "Alice J. Smith" }),
		).resolves.toEqual(updated);
		expect(mockPatch).toHaveBeenCalledWith("/admin/users/u1", {
			full_name: "Alice J. Smith",
		});
	});

	it("deletes user by id", async () => {
		mockDelete.mockResolvedValue({});

		await expect(deleteUser("u1")).resolves.toBeUndefined();
		expect(mockDelete).toHaveBeenCalledWith("/admin/users/u1");
	});

	it("patches user password by id", async () => {
		mockPatch.mockResolvedValue({});

		await expect(resetUserPassword("u1", "NewP@ss1!")).resolves.toBeUndefined();
		expect(mockPatch).toHaveBeenCalledWith("/admin/users/u1/password", {
			new_password: "NewP@ss1!",
		});
	});

	it("exposes usersQueryOptions query key contract", () => {
		const opts = usersQueryOptions(2, 10);
		expect(opts.queryKey).toEqual(["admin", "users", 2, 10]);
		expect(typeof opts.queryFn).toBe("function");
	});

	it("usersQueryOptions defaults to page 1 and page_size 20", () => {
		const opts = usersQueryOptions();
		expect(opts.queryKey).toEqual(["admin", "users", 1, 20]);
	});

	// ── Regression: "add team member" picker showed no users ──────────────────
	// Root cause was requesting page_size=500, which the backend rejects (cap 100).
	it("ADMIN_USERS_PAGE_SIZE stays within the backend page_size cap", () => {
		expect(ADMIN_USERS_PAGE_SIZE).toBeGreaterThan(0);
		expect(ADMIN_USERS_PAGE_SIZE).toBeLessThanOrEqual(
			ADMIN_USERS_MAX_PAGE_SIZE,
		);
	});

	it("usersInfiniteQueryOptions requests a page_size the backend accepts", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: {
					items: [],
					total: 0,
					page: 1,
					page_size: ADMIN_USERS_PAGE_SIZE,
				},
				error_code: null,
				message: "ok",
			},
		});

		const opts = usersInfiniteQueryOptions();
		expect(opts.queryKey).toEqual(["admin", "users", "all"]);
		// biome-ignore lint/suspicious/noExplicitAny: exercising the queryFn contract
		await (opts.queryFn as any)({ pageParam: opts.initialPageParam });

		const [, config] = mockGet.mock.calls[0];
		expect(config.params.page_size).toBeLessThanOrEqual(
			ADMIN_USERS_MAX_PAGE_SIZE,
		);
	});

	it("usersInfiniteQueryOptions stops paging once all users are loaded", () => {
		const opts = usersInfiniteQueryOptions();
		// last page still has room → fetch next
		expect(
			opts.getNextPageParam(
				{ items: [], total: 50, page: 1, page_size: 20 },
				[],
				1,
				[],
			),
		).toBe(2);
		// all users loaded → stop
		expect(
			opts.getNextPageParam(
				{ items: [], total: 20, page: 1, page_size: 20 },
				[],
				1,
				[],
			),
		).toBeUndefined();
	});
});
