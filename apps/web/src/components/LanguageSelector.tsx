import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const languages = [
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
  { code: "pt-BR", name: "Português (Brasil)", flag: "🇧🇷" },
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
  { code: "ru-RU", name: "Русский", flag: "🇷🇺" },
  { code: "zh-CN", name: "简体中文", flag: "🇨🇳" },
  { code: "pl-PL", name: "Polski", flag: "🇵🇱" },
  { code: "et-EE", name: "Eesti", flag: "🇪🇪" },
  { code: "vi-VN", name: "Tiếng Việt", flag: "🇻🇳" },
  { code: "zh-TW", name: "繁體中文 (台灣)", flag: "🇹🇼" },
  { code: "uk-UA", name: "Українська", flag: "🇺🇦" },
  { code: "th-TH", name: "ไทย", flag: "🇹🇭" },
  { code: "el-GR", name: "Ελληνικά", flag: "🇬🇷" },
  { code: "yue-Hant-HK", name: "粵語 (廣東話)", flag: "🇭🇰" }, // Cantonese, Hong Kong
  { code: "ro-RO", name: "Română", flag: "🇷🇴" },
  { code: "ur-PK", name: "اردو", flag: "🇵🇰" },
  { code: "ka-GE", name: "ქართული", flag: "🇬🇪" },
  { code: "uz-UZ", name: "Oʻzbekcha", flag: "🇺🇿" },
  { code: "ga-IE", name: "Gaeilge", flag: "🇮🇪" },
];

// const languages = [
//   { code: "fr", name: "Français", flag: "🇫🇷" },
//   { code: "ua", name: "Українська", flag: "🇺🇦" },
//   { code: "cn", name: "中文", flag: "🇨🇳" },
// ];

// const languageList = {
//   "ar-SY": "العربية",
//   "cs-CZ": "Čeština",
//   "zh-HK": "繁體中文 (香港)",
//   "bg-BG": "Български",
//   "be": "Беларуская",
//   "de-DE": "Deutsch (Deutschland)",
//   "de-CH": "Deutsch (Schweiz)",
//   "nl-NL": "Nederlands",
//   "nb-NO": "Norsk",
//   "es-ES": "Español",
//   "eu": "Euskara",
//   "fa": "Farsi",
//   "pt-PT": "Português (Portugal)",
//   "pt-BR": "Português (Brasileiro)",
//   "fi": "Suomi",
//   "fr-FR": "Français (France)",
//   "he-IL": "עברית",
//   "hu": "Magyar",
//   "hr-HR": "Hrvatski",
//   "it-IT": "Italiano (Italian)",
//   "id-ID": "Bahasa Indonesia (Indonesian)",
//   "ja": "日本語",
//   "da-DK": "Danish (Danmark)",
//   "sr": "Српски",
//   "sl-SI": "Slovenščina",
//   "sr-latn": "Srpski",
//   "sv-SE": "Svenska",
//   "tr-TR": "Türkçe",
//   "ko-KR": "한국어",
//   "lt": "Lietuvių",
//   "ru-RU": "Русский",
//   "zh-CN": "简体中文",
//   "pl": "Polski",
//   "et-EE": "eesti",
//   "vi-VN": "Tiếng Việt",
//   "zh-TW": "繁體中文 (台灣)",
//   "uk-UA": "Українська",
//   "th-TH": "ไทย",
//   "el-GR": "Ελληνικά",
//   "yue": "繁體中文 (廣東話 / 粵語)",
//   "ro": "Limba română",
//   "ur": "Urdu",
//   "ge": "ქართული",
//   "uz": "O'zbek tili",
//   "ga": "Gaeilge",
// };

export function LanguageSelector() {
  const { getCurrentLanguage, changeLanguage } = useLocalizedTranslation();
  const currentLanguage = getCurrentLanguage();

  const currentLang =
    languages.find((lang) => lang.code === currentLanguage) || languages[0];

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

      <SelectContent>
        {languages.map((language) => (
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
