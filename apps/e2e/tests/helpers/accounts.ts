import {
	type APIRequestContext,
	type APIResponse,
	expect,
} from "@playwright/test";
import { API_URL, authRequest } from "./e2e-api";

// Helpers for the account kinds beyond users: service accounts and the API keys
// every kind of account authenticates with. See docs/api/service-accounts.md.

export interface ServiceAccountRecord {
	id: string;
	name: string;
	description: string;
	created_by?: string;
	roles: Array<{ id: string; name: string }>;
}

export interface MintedKey {
	id: string;
	name: string;
	key_prefix: string;
	key: string;
}

/** Creates a service account as `request`'s user; returns the raw response. */
export function postServiceAccount(
	request: APIRequestContext,
	name: string,
	description?: string,
): Promise<APIResponse> {
	return request.post(`${API_URL}/admin/service-accounts`, {
		data: { name, ...(description === undefined ? {} : { description }) },
	});
}

/** Creates a service account and returns it. */
export async function createServiceAccount(
	request: APIRequestContext,
	name: string,
	description?: string,
): Promise<ServiceAccountRecord> {
	const response = await postServiceAccount(request, name, description);
	expect(response.status(), await response.text()).toBe(201);
	return (await response.json()).data as ServiceAccountRecord;
}

/** Mints a key for a service account; returns the raw response. */
export function postServiceAccountKey(
	request: APIRequestContext,
	serviceAccountId: string,
	name: string,
	expiresAt?: string,
): Promise<APIResponse> {
	return request.post(
		`${API_URL}/admin/service-accounts/${serviceAccountId}/api-keys`,
		{ data: { name, ...(expiresAt ? { expires_at: expiresAt } : {}) } },
	);
}

/** Mints a key for a service account and returns it (the raw key is shown once). */
export async function createServiceAccountKey(
	request: APIRequestContext,
	serviceAccountId: string,
	name = "e2e key",
	expiresAt?: string,
): Promise<MintedKey> {
	const response = await postServiceAccountKey(
		request,
		serviceAccountId,
		name,
		expiresAt,
	);
	expect(response.status(), await response.text()).toBe(201);
	return (await response.json()).data as MintedKey;
}

/** Replaces a service account's workspace roles; returns the raw response. */
export function putServiceAccountRoles(
	request: APIRequestContext,
	serviceAccountId: string,
	roleIds: string[],
): Promise<APIResponse> {
	return request.put(
		`${API_URL}/admin/service-accounts/${serviceAccountId}/roles`,
		{ data: { role_ids: roleIds } },
	);
}

/** The ids of every live service account whose name starts with `prefix`. */
export async function listServiceAccounts(
	request: APIRequestContext,
): Promise<ServiceAccountRecord[]> {
	const response = await request.get(`${API_URL}/admin/service-accounts`);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data ?? [];
}

export async function cleanupServiceAccountsByPrefix(
	request: APIRequestContext,
	prefix: string,
): Promise<void> {
	await authRequest(request);
	const accounts = await listServiceAccounts(request);
	await Promise.all(
		accounts
			.filter((a) => a.name.startsWith(prefix))
			.map((a) => request.delete(`${API_URL}/admin/service-accounts/${a.id}`)),
	);
}

/** The status of GET /admin/roles with `api`: 200 when it holds roles:read, 403 when not, 401 when unauthenticated. */
export async function rolesReadStatus(api: APIRequestContext): Promise<number> {
	return (await api.get(`${API_URL}/admin/roles`)).status();
}
