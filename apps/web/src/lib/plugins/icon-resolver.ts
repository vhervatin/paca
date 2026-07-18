import { icons, Puzzle } from "lucide-react";
import type { ComponentType } from "react";

/**
 * Resolves a lucide-react icon by its PascalCase export name (as referenced
 * in a plugin manifest's `navItems[].icon`, e.g. "Clock", "BarChart3").
 * Falls back to a generic puzzle-piece icon for unknown/omitted names so a
 * typo in a plugin manifest never breaks the sidebar.
 */
export function resolvePluginIcon(
	name?: string,
): ComponentType<{ className?: string }> {
	if (!name) return Puzzle;
	return (
		(icons as Record<string, ComponentType<{ className?: string }>>)[name] ??
		Puzzle
	);
}
