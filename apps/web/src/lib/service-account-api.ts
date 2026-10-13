import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import type {
	APIKey,
	CreateAPIKeyRequest,
	CreateAPIKeyResponse,
} from "./apikey-api";
import type { Role, RoleSummary } from "./role-api";

/**
 * A service account: a workspace-level, non-human account that authenticates
 * with its own API keys and holds roles like a user, but cannot sign in.
 */
export interface ServiceAccount {
	id: string;
	name: string;
	description: string;
	created_by?: string;
	/** The workspace roles the account holds, sorted by name. */
	roles: RoleSummary[];
	created_at: string;
	updated_at: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
}

export interface CreateServiceAccountRequest {
	name: string;
	description?: string;
}

export interface UpdateServiceAccountRequest {
	name?: string;
	description?: string;
}

const BASE = "/admin/service-accounts";

export async function listServiceAccounts(): Promise<ServiceAccount[]> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<ServiceAccount[]>>(BASE);
	return data.data;
}

export async function getServiceAccount(id: string): Promise<ServiceAccount> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ServiceAccount>
	>(`${BASE}/${id}`);
	return data.data;
}

export async function createServiceAccount(
	payload: CreateServiceAccountRequest,
): Promise<ServiceAccount> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ServiceAccount>
	>(BASE, payload);
	return data.data;
}

export async function updateServiceAccount(
	id: string,
	payload: UpdateServiceAccountRequest,
): Promise<ServiceAccount> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<ServiceAccount>
	>(`${BASE}/${id}`, payload);
	return data.data;
}

export async function deleteServiceAccount(id: string): Promise<void> {
	await apiClient.instance.delete(`${BASE}/${id}`);
}

export async function listServiceAccountKeys(id: string): Promise<APIKey[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<APIKey[]>>(
		`${BASE}/${id}/api-keys`,
	);
	return data.data;
}

export async function createServiceAccountKey(
	id: string,
	payload: CreateAPIKeyRequest,
): Promise<CreateAPIKeyResponse> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CreateAPIKeyResponse>
	>(`${BASE}/${id}/api-keys`, payload);
	return data.data;
}

export async function revokeServiceAccountKey(
	id: string,
	keyId: string,
): Promise<void> {
	await apiClient.instance.delete(`${BASE}/${id}/api-keys/${keyId}`);
}

export async function listServiceAccountRoles(id: string): Promise<Role[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Role[]>>(
		`${BASE}/${id}/roles`,
	);
	return data.data;
}

export async function replaceServiceAccountRoles(
	id: string,
	roleIds: string[],
): Promise<Role[]> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role[]>>(
		`${BASE}/${id}/roles`,
		{ role_ids: roleIds },
	);
	return data.data;
}

export const serviceAccountsQueryOptions = queryOptions({
	queryKey: ["admin", "service-accounts"],
	queryFn: listServiceAccounts,
});

export const serviceAccountQueryOptions = (id: string) =>
	queryOptions({
		queryKey: ["admin", "service-accounts", id],
		queryFn: () => getServiceAccount(id),
	});

export const serviceAccountKeysQueryOptions = (id: string) =>
	queryOptions({
		queryKey: ["admin", "service-accounts", id, "api-keys"],
		queryFn: () => listServiceAccountKeys(id),
	});

export const serviceAccountRolesQueryOptions = (id: string) =>
	queryOptions({
		queryKey: ["admin", "service-accounts", id, "roles"],
		queryFn: () => listServiceAccountRoles(id),
	});
