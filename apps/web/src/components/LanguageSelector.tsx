import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const languages = [
  { code: "en", name: "English", flag: "🇺🇸" },
  { code: "fr", name: "Français", flag: "🇫🇷" },
  { code: "ua", name: "Українська", flag: "🇺🇦" },
];

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
            <span className="hidden sm:inline">{currentLang.name}</span>
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
