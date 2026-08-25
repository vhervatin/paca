import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/projects/$projectId/")({
	beforeLoad: ({ params }) => {
		throw redirect({
			to: "/projects/$projectId/interactions/backlog",
			params: { projectId: params.projectId },
		});
	},
});
