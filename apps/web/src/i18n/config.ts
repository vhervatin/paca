import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import { onStorageKeyChange } from "@/lib/storage-sync";

export const SUPPORTED_LANGUAGES = [
	"pt-BR",
	"en",
	"vi",
	"ko",
	"zh-CN",
	"ja",
	"es",
	"fr",
	"ru",
] as const;

export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

export const LOCALE_LABELS: Record<SupportedLanguage, string> = {
	"pt-BR": "Português (Brasil)",
	en: "English",
	vi: "Tiếng Việt",
	ko: "한국어",
	"zh-CN": "简体中文",
	ja: "日本語",
	es: "Español",
	fr: "Français",
	ru: "Русский",
};

export const LOCALE_STORAGE_KEY = "locale";

// "en" ships eagerly — it's the fallbackLng, so it must always be available
// synchronously. Every other language is fetched on demand via loadLanguage()
// below. Eagerly globbing all nine locales here used to bundle ~1.2MB of
// translation JSON into main.tsx's entry chunk on every page load, regardless
// of which single language a given session actually uses.
const enModules = import.meta.glob("./locales/en/*.json", {
	eager: true,
}) as Record<string, { default: Record<string, unknown> }>;

// EVUP: pt-BR também é carregado eagerly — é o idioma padrão e o primeiro
// fallback, então precisa estar disponível de forma síncrona (mesmo motivo do
// "en" acima). Os outros 7 idiomas continuam sob demanda via loadLanguage().
const ptBRModules = import.meta.glob("./locales/pt-BR/*.json", {
	eager: true,
}) as Record<string, { default: Record<string, unknown> }>;

const otherLocaleLoaders = import.meta.glob("./locales/*/*.json") as Record<
	string,
	() => Promise<{ default: Record<string, unknown> }>
>;

function namespaceFromPath(path: string): string {
	return path.match(/\/([^/]+)\.json$/)?.[1] ?? "";
}

const enResources: Record<string, Record<string, unknown>> = {};
for (const [path, mod] of Object.entries(enModules)) {
	enResources[namespaceFromPath(path)] = mod.default;
}

const ptBRResources: Record<string, Record<string, unknown>> = {};
for (const [path, mod] of Object.entries(ptBRModules)) {
	ptBRResources[namespaceFromPath(path)] = mod.default;
}

export function resolveLocale(language: string): SupportedLanguage {
	const exact = SUPPORTED_LANGUAGES.find((code) => code === language);
	if (exact) return exact;

	const base = language.split("-")[0];
	const baseMatch = SUPPORTED_LANGUAGES.find((code) => code === base);
	return baseMatch ?? "en";
}

const loadedLanguages = new Set<SupportedLanguage>(["en", "pt-BR"]);

/**
 * Fetches and registers a language's namespace bundles, once per language.
 * "en" is already loaded eagerly (see enResources above), so this is a no-op
 * for it.
 */
export async function loadLanguage(lang: SupportedLanguage): Promise<void> {
	if (loadedLanguages.has(lang)) return;

	const entries = Object.entries(otherLocaleLoaders).filter(([path]) =>
		path.startsWith(`./locales/${lang}/`),
	);
	const modules = await Promise.all(
		entries.map(async ([path, load]) => {
			const mod = await load();
			return [namespaceFromPath(path), mod.default] as const;
		}),
	);
	for (const [namespace, data] of modules) {
		i18next.addResourceBundle(lang, namespace, data, true, true);
	}
	loadedLanguages.add(lang);
}

function detectInitialLanguage(): SupportedLanguage {
	if (typeof window === "undefined") return "pt-BR";
	const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
	if (stored) return resolveLocale(stored);
	// EVUP: sem escolha explícita salva, o padrão é pt-BR — não seguimos o
	// idioma do navegador (senão um navegador em inglês abriria em inglês).
	return "pt-BR";
}

const initialLanguage = detectInitialLanguage();

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: { en: enResources, "pt-BR": ptBRResources },
		lng: initialLanguage,
		// EVUP: pt-BR é o fallback primário; "en" fica como rede de segurança
		// para chaves ainda não traduzidas.
		fallbackLng: ["pt-BR", "en"],
		supportedLngs: SUPPORTED_LANGUAGES,
		defaultNS: "common",
		interpolation: {
			escapeValue: false,
		},
		detection: {
			// pt-BR é o padrão: só respeitamos escolha explícita salva no
			// localStorage; não seguimos o idioma do navegador (senão um
			// navegador em inglês abriria em inglês em vez de pt-BR).
			order: ["localStorage"],
			lookupLocalStorage: LOCALE_STORAGE_KEY,
			caches: ["localStorage"],
		},
	});

// Resolves once the *initial* language's resources are ready. "en" resolves
// immediately since it's bundled eagerly; any other language waits on one
// dynamic import. main.tsx awaits this before its first render so components
// never flash untranslated/raw keys.
export const i18nReady: Promise<void> =
	initialLanguage === "en" || initialLanguage === "pt-BR"
		? Promise.resolve()
		: loadLanguage(initialLanguage);

if (typeof window !== "undefined") {
	onStorageKeyChange(LOCALE_STORAGE_KEY, (event) => {
		if (event.newValue && event.newValue !== i18next.language) {
			const next = resolveLocale(event.newValue);
			loadLanguage(next)
				.then(() => i18next.changeLanguage(next))
				.catch((error) => {
					console.error(`i18n: failed to load language "${next}"`, error);
				});
		}
	});

	const syncHtmlLang = (language: string) => {
		document.documentElement.lang = language;
	};
	syncHtmlLang(i18next.language);
	i18next.on("languageChanged", syncHtmlLang);
}

export default i18next;
