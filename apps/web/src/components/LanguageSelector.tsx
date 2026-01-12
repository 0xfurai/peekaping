import {
    Select,
    SelectContent,
    SelectItem,
    SelectSeparator,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const allLanguages = [
    { code: "pt-BR", name: "Português (Brasil)", flag: "🇧🇷" },
    { code: "en-US", name: "English", flag: "🇺🇸" },
    { code: "ar-SY", name: "العربية", flag: "🇸🇾" },
    { code: "cs-CZ", name: "Čeština", flag: "🇨🇿" },
    { code: "zh-HK", name: "繁體中文 (香港)", flag: "🇭🇰" },
    { code: "bg-BG", name: "Български", flag: "🇧🇬" },
    { code: "be-BY", name: "Беларуская", flag: "🇧🇾" },
    { code: "de-DE", name: "Deutsch (Deutschland)", flag: "🇩🇪" },
    { code: "de-CH", name: "Deutsch (Schweiz)", flag: "🇨🇭" },
    { code: "nl-NL", name: "Nederlands", flag: "🇳🇱" },
    { code: "nb-NO", name: "Norsk (Bokmål)", flag: "🇳🇴" },
    { code: "es-ES", name: "Español", flag: "🇪🇸" },
    { code: "eu-ES", name: "Euskara", flag: "🏴" }, // Basque — no ISO country, fallback flag
    { code: "fa-IR", name: "فارسی", flag: "🇮🇷" },
    { code: "pt-PT", name: "Português (Portugal)", flag: "🇵🇹" },
    { code: "fi-FI", name: "Suomi", flag: "🇫🇮" },
    { code: "fr-FR", name: "Français", flag: "🇫🇷" },
    { code: "he-IL", name: "עברית", flag: "🇮🇱" },
    { code: "hu-HU", name: "Magyar", flag: "🇭🇺" },
    { code: "hr-HR", name: "Hrvatski", flag: "🇭🇷" },
    { code: "it-IT", name: "Italiano", flag: "🇮🇹" },
    { code: "id-ID", name: "Bahasa Indonesia", flag: "🇮🇩" },
    { code: "ja-JP", name: "日本語", flag: "🇯🇵" },
    { code: "da-DK", name: "Danish (Danmark)", flag: "🇩🇰" },
    { code: "sr-Cyrl", name: "Српски (Ћирилица)", flag: "🇷🇸" },
    { code: "sr-Latn", name: "Srpski (Latinica)", flag: "🇷🇸" },
    { code: "sl-SI", name: "Slovenščina", flag: "🇸🇮" },
    { code: "sv-SE", name: "Svenska", flag: "🇸🇪" },
    { code: "tr-TR", name: "Türkçe", flag: "🇹🇷" },
    { code: "ko-KR", name: "한국어", flag: "🇰🇷" },
    { code: "lt-LT", name: "Lietuvių", flag: "🇱🇹" },
    { code: "zh-CN", name: "简体中文", flag: "🇨🇳" },
    { code: "pl-PL", name: "Polski", flag: "🇵🇱" },
    { code: "et-EE", name: "Eesti", flag: "🇪🇪" },
    { code: "vi-VN", name: "Tiếng Việt", flag: "🇻🇳" },
    { code: "zh-TW", name: "繁體中文 (台灣)", flag: "🇹🇼" },
    { code: "uk-UA", name: "Українська", flag: "🇺🇦" },
    { code: "th-TH", name: "ไทย", flag: "🇹🇭" },
    { code: "el-GR", name: "Ελληνικά", flag: "🇬🇷" },
    { code: "yue", name: "粵語 (廣東話)", flag: "🇭🇰" }, // Cantonese, Hong Kong
    { code: "ro-RO", name: "Română", flag: "🇷🇴" },
    { code: "ur-PK", name: "اردو", flag: "🇵🇰" },
    { code: "ka-GE", name: "ქართული", flag: "🇬🇪" },
    { code: "uz-UZ", name: "Oʻzbekcha", flag: "🇺🇿" },
    { code: "ga-IE", name: "Gaeilge", flag: "🇮🇪" },
];

export function LanguageSelector() {
    const { getCurrentLanguage, changeLanguage } = useLocalizedTranslation();
    const currentLanguage = getCurrentLanguage();

    const currentLang =
        allLanguages.find((lang) => lang.code === currentLanguage) || allLanguages[0];

    const mainLanguages = allLanguages.slice(0, 2);
    const otherLanguages = allLanguages.slice(2);

    return (
        <Select value={currentLanguage} onValueChange={changeLanguage}>
            <SelectTrigger className="w-full">
                <SelectValue>
                    <div className="flex items-center gap-2">
                        <span>{currentLang.flag}</span>
                        <span className="">{currentLang.name}</span>
                    </div>
                </SelectValue>
            </SelectTrigger>

            <SelectContent className="max-h-[220px]">
                {mainLanguages.map((language) => (
                    <SelectItem key={language.code} value={language.code}>
                        <div className="flex items-center gap-2">
                            <span>{language.flag}</span>
                            <span>{language.name}</span>
                        </div>
                    </SelectItem>
                ))}

                <SelectSeparator />

                {otherLanguages.map((language) => (
                    <SelectItem key={language.code} value={language.code}>
                        <div className="flex items-center gap-2">
                            <span>{language.flag}</span>
                            <span>{language.name}</span>
                        </div>
                    </SelectItem>
                ))}
            </SelectContent>
        </Select>
    );
}
