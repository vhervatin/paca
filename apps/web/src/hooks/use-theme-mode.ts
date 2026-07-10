import { useEffect, useState } from "react";
import { onStorageKeyChange } from "@/lib/storage-sync";

export type ThemeMode = "light" | "dark" | "auto";

const THEME_STORAGE_KEY = "theme";
const THEME_CHANGE_EVENT = "paca-theme-change";

function getSystemPrefersDark(): boolean {
	if (typeof window === "undefined") {
		return false;
	}

	return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function getStoredTheme(): ThemeMode {
	if (typeof window === "undefined") {
		return "dark";
	}

	const value = window.localStorage.getItem(THEME_STORAGE_KEY);
	return value === "light" || value === "dark" || value === "auto"
		? value
		: "dark";
}

function applyTheme(mode: ThemeMode) {
	const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
	const resolved = mode === "auto" ? (prefersDark ? "dark" : "light") : mode;

	document.documentElement.classList.remove("light", "dark");
	document.documentElement.classList.add(resolved);

	if (mode === "auto") {
		document.documentElement.removeAttribute("data-theme");
	} else {
		document.documentElement.setAttribute("data-theme", mode);
	}

	document.documentElement.style.colorScheme = resolved;
}

export function useThemeMode() {
	const [mode, setMode] = useState<ThemeMode>(getStoredTheme);
	const [prefersDark, setPrefersDark] = useState(getSystemPrefersDark);

	useEffect(() => {
		const onThemeChange = (event: Event) => {
			const detail = (event as CustomEvent<ThemeMode>).detail;
			if (detail === "light" || detail === "dark" || detail === "auto") {
				setMode(detail);
			}
		};

		window.addEventListener(THEME_CHANGE_EVENT, onThemeChange as EventListener);
		const offStorage = onStorageKeyChange(THEME_STORAGE_KEY, () => {
			setMode(getStoredTheme());
		});

		return () => {
			window.removeEventListener(
				THEME_CHANGE_EVENT,
				onThemeChange as EventListener,
			);
			offStorage();
		};
	}, []);

	useEffect(() => {
		const media = window.matchMedia("(prefers-color-scheme: dark)");
		const onSystemChange = (event: MediaQueryListEvent) => {
			setPrefersDark(event.matches);
		};

		media.addEventListener("change", onSystemChange);
		return () => media.removeEventListener("change", onSystemChange);
	}, []);

	useEffect(() => {
		if (mode !== "auto") {
			return;
		}

		const media = window.matchMedia("(prefers-color-scheme: dark)");
		const onChange = () => {
			applyTheme("auto");
			window.dispatchEvent(
				new CustomEvent<ThemeMode>(THEME_CHANGE_EVENT, { detail: "auto" }),
			);
		};

		media.addEventListener("change", onChange);
		return () => media.removeEventListener("change", onChange);
	}, [mode]);

	function set(next: ThemeMode) {
		setMode(next);
		applyTheme(next);
		window.localStorage.setItem(THEME_STORAGE_KEY, next);
		window.dispatchEvent(
			new CustomEvent<ThemeMode>(THEME_CHANGE_EVENT, { detail: next }),
		);
	}

	const resolvedMode: Exclude<ThemeMode, "auto"> =
		mode === "auto" ? (prefersDark ? "dark" : "light") : mode;

	return { mode, resolvedMode, set };
}
