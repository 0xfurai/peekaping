import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

// Only import English and Portuguese as the default/fallback languages
import enUS from "./locales/en-US.json";
import ptBR from "./locales/pt-BR.json";

// Available languages configuration
export const AVAILABLE_LANGUAGES = [
    "en-US", "ar-SY", "cs-CZ", "zh-HK", "bg-BG", "be-BY", "de-DE", "de-CH",
    "nl-NL", "nb-NO", "es-ES", "eu-ES", "fa-IR", "pt-PT", "pt-BR", "fi-FI",
    "fr-FR", "he-IL", "hu-HU", "hr-HR", "it-IT", "id-ID", "ja-JP", "da-DK",
    "sr-Cyrl", "sr-Latn", "sl-SI", "sv-SE", "tr-TR", "ko-KR", "lt-LT",
    "zh-CN", "pl-PL", "et-EE", "vi-VN", "zh-TW", "uk-UA", "th-TH", "el-GR",
    "yue", "ro-RO", "ur-PK", "ka-GE", "uz-UZ", "ga-IE"
];

// Cache for loaded translations
const loadedLanguages = new Set<string>();
const loadingPromises = new Map<string, Promise<Record<string, unknown>>>();

// Dynamic import function for language files
export const loadLanguage = async (languageCode: string): Promise<void> => {
    // If already loaded, return immediately
    if (loadedLanguages.has(languageCode)) {
        return;
    }

    // If currently loading, return the existing promise
    if (loadingPromises.has(languageCode)) {
        await loadingPromises.get(languageCode);
        return;
    }

    // If not a valid language code, throw error
    if (!AVAILABLE_LANGUAGES.includes(languageCode)) {
        throw new Error(`Language ${languageCode} is not supported`);
    }

    // Create loading promise
    const loadingPromise = (async () => {
        try {
            const translation = await import(`./locales/${languageCode}.json`);

            // Add the resource to i18n
            i18n.addResourceBundle(languageCode, 'translation', translation.default, true, true);

            // Mark as loaded
            loadedLanguages.add(languageCode);

            return translation.default;
        } catch (error) {
            console.error(`Failed to load language ${languageCode}:`, error);
            throw error;
        } finally {
            // Clean up loading promise
            loadingPromises.delete(languageCode);
        }
    })();

    // Store the promise
    loadingPromises.set(languageCode, loadingPromise);

    // Wait for loading to complete
    await loadingPromise;
};

// Initial resources with English and Portuguese
const resources = {
    "en-US": { translation: enUS },
    "pt-BR": { translation: ptBR },
};

// Mark languages as loaded
loadedLanguages.add("en-US");
loadedLanguages.add("pt-BR");

// Initialize i18n with basic configuration
i18n
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
        resources,
        fallbackLng: "pt-BR",
        debug: false,
        interpolation: {
            escapeValue: false, // React already escapes values
        },
        detection: {
            order: ["localStorage", "htmlTag"], // Removed navigator to enforce default if no storage
            caches: ["localStorage"],
        },
    });

// Load the detected language after initialization
const initializeDetectedLanguage = async () => {
    const detectedLanguage = i18n.language || "pt-BR";

    // If the detected language is not loaded and is supported, load it
    if (detectedLanguage !== "en-US" && detectedLanguage !== "pt-BR" && AVAILABLE_LANGUAGES.includes(detectedLanguage)) {
        try {
            await loadLanguage(detectedLanguage);
            // Force i18n to use the loaded language
            await i18n.changeLanguage(detectedLanguage);
        } catch (error) {
            console.error(`Failed to load detected language ${detectedLanguage}:`, error);
            // Fallback
            await i18n.changeLanguage("pt-BR");
        }
    }
};

// Initialize detected language when i18n is ready
i18n.on('initialized', () => {
    initializeDetectedLanguage();
});

// If i18n is already initialized, run immediately
if (i18n.isInitialized) {
    initializeDetectedLanguage();
}

export default i18n;
