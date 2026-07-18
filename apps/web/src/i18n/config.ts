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

const localeModules = import.meta.glob("./locales/*/*.json", {
	eager: true,
}) as Record<string, { default: Record<string, unknown> }>;

const resources: Record<string, Record<string, Record<string, unknown>>> = {};

for (const [path, mod] of Object.entries(localeModules)) {
	const match = path.match(/\.\/locales\/([^/]+)\/([^/]+)\.json$/);
	if (!match) continue;
	const [, lang, namespace] = match;
	resources[lang] ??= {};
	resources[lang][namespace] = mod.default;
}

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources,
		fallbackLng: "pt-BR",
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

if (typeof window !== "undefined") {
	onStorageKeyChange(LOCALE_STORAGE_KEY, (event) => {
		if (event.newValue && event.newValue !== i18next.language) {
			void i18next.changeLanguage(event.newValue);
		}
	});

	const syncHtmlLang = (language: string) => {
		document.documentElement.lang = language;
	};
	syncHtmlLang(i18next.language);
	i18next.on("languageChanged", syncHtmlLang);
}

export default i18next;
